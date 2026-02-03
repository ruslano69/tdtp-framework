package packet

import (
	"context"
	"fmt"
	"strings"
)

// StreamingGenerator генерирует TDTP пакеты в потоковом режиме
// В отличие от обычного Generator, не требует загрузки всех данных в память
type StreamingGenerator struct {
	*Generator
	partSizeBytes int // Максимальный размер одной части в байтах
}

// NewStreamingGenerator создает новый потоковый генератор
func NewStreamingGenerator() *StreamingGenerator {
	return &StreamingGenerator{
		Generator:     NewGenerator(),
		partSizeBytes: 3800000, // ~3.8MB для получения ~1.9MB XML
	}
}

// SetPartSize устанавливает максимальный размер части в байтах
func (sg *StreamingGenerator) SetPartSize(size int) {
	sg.partSizeBytes = size
}

// PartResult представляет результат генерации одной части
type PartResult struct {
	Packet    *DataPacket // Сгенерированный пакет
	PartNum   int         // Номер части (1-based)
	RowsCount int         // Количество строк в этой части
	Error     error       // Ошибка генерации (если есть)
}

// StreamingSummary содержит итоговую информацию о потоковом экспорте
type StreamingSummary struct {
	MessageIDBase string // Базовый MessageID для всех частей
	TotalParts    int    // Фактическое количество частей
	TotalRows     int    // Общее количество строк
}

// GeneratePartsStream генерирует части по мере чтения данных из канала
//
// Ключевые особенности:
// - Не требует загрузки всех данных в память
// - TotalParts = 0 в каждой части (unknown до конца экспорта)
// - Части генерируются по мере поступления данных
// - В конце возвращается StreamingSummary с фактическими значениями
//
// Использование:
//
//	rowsChan := make(chan []string)
//	go func() {
//	    // Читаем данные из БД и отправляем в канал
//	    for rows.Next() {
//	        rowsChan <- scanRow()
//	    }
//	    close(rowsChan)
//	}()
//
//	partsChan, summaryChan := sg.GeneratePartsStream(ctx, rowsChan, schema, "users", TypeReference)
//	for part := range partsChan {
//	    if part.Error != nil {
//	        log.Error(part.Error)
//	        continue
//	    }
//	    broker.Send(ctx, part.Packet)
//	}
//	summary := <-summaryChan
func (sg *StreamingGenerator) GeneratePartsStream(
	ctx context.Context,
	rowsChan <-chan []string,
	schema Schema,
	tableName string,
	msgType MessageType,
) (<-chan *PartResult, <-chan *StreamingSummary) {
	partsChan := make(chan *PartResult, 1)
	summaryChan := make(chan *StreamingSummary, 1)

	go func() {
		defer close(partsChan)
		defer close(summaryChan)

		messageIDBase := sg.generateMessageID(msgType)
		partNum := 1
		totalRows := 0

		currentPartRows := [][]string{}
		currentSize := 0
		overheadSize := 5000 // Примерный размер служебной информации

		for {
			select {
			case <-ctx.Done():
				partsChan <- &PartResult{
					Error: ctx.Err(),
				}
				// Отправляем summary даже при отмене контекста
				summaryChan <- &StreamingSummary{
					MessageIDBase: messageIDBase,
					TotalParts:    partNum - 1, // Уже созданные части
					TotalRows:     totalRows,
				}
				return

			case row, ok := <-rowsChan:
				if !ok {
					// Канал закрыт, генерируем последнюю часть если есть данные
					if len(currentPartRows) > 0 {
						packet, err := sg.createPart(
							messageIDBase,
							partNum,
							0, // TotalParts unknown
							tableName,
							msgType,
							schema,
							currentPartRows,
						)

						partsChan <- &PartResult{
							Packet:    packet,
							PartNum:   partNum,
							RowsCount: len(currentPartRows),
							Error:     err,
						}

						totalRows += len(currentPartRows)
					}

					// Отправляем итоговую информацию
					summaryChan <- &StreamingSummary{
						MessageIDBase: messageIDBase,
						TotalParts:    partNum,
						TotalRows:     totalRows,
					}
					return
				}

				rowSize := sg.estimateRowSize(row)

				// Проверяем нужно ли начать новую часть
				if currentSize+rowSize+overheadSize > sg.partSizeBytes && len(currentPartRows) > 0 {
					// Генерируем текущую часть
					packet, err := sg.createPart(
						messageIDBase,
						partNum,
						0, // TotalParts unknown в streaming режиме
						tableName,
						msgType,
						schema,
						currentPartRows,
					)

					partsChan <- &PartResult{
						Packet:    packet,
						PartNum:   partNum,
						RowsCount: len(currentPartRows),
						Error:     err,
					}

					if err != nil {
						// При ошибке отправляем summary и завершаем
						summaryChan <- &StreamingSummary{
							MessageIDBase: messageIDBase,
							TotalParts:    partNum, // Части до ошибки
							TotalRows:     totalRows,
						}
						return
					}

					totalRows += len(currentPartRows)
					partNum++
					currentPartRows = [][]string{}
					currentSize = 0
				}

				// Добавляем строку в текущую часть
				currentPartRows = append(currentPartRows, row)
				currentSize += rowSize
			}
		}
	}()

	return partsChan, summaryChan
}

// createPart создает DataPacket для одной части
func (sg *StreamingGenerator) createPart(
	messageIDBase string,
	partNum int,
	totalParts int,
	tableName string,
	msgType MessageType,
	schema Schema,
	rows [][]string,
) (*DataPacket, error) {
	packet := NewDataPacket(msgType, tableName)
	packet.Header.MessageID = fmt.Sprintf("%s-P%d", messageIDBase, partNum)
	packet.Header.PartNumber = partNum
	packet.Header.TotalParts = totalParts // 0 = unknown в streaming режиме
	packet.Header.RecordsInPart = len(rows)

	// Schema во всех частях (для самодостаточности)
	packet.Schema = schema

	// Преобразуем строки в Data
	packet.Data = sg.rowsToData(rows)

	return packet, nil
}

// GeneratePartsStreamWithSender генерирует части с указанием sender/recipient
// Используется для Response пакетов
func (sg *StreamingGenerator) GeneratePartsStreamWithSender(
	ctx context.Context,
	rowsChan <-chan []string,
	schema Schema,
	tableName string,
	msgType MessageType,
	inReplyTo string,
	sender string,
	recipient string,
) (<-chan *PartResult, <-chan *StreamingSummary) {
	partsChan := make(chan *PartResult, 1)
	summaryChan := make(chan *StreamingSummary, 1)

	go func() {
		defer close(partsChan)
		defer close(summaryChan)

		messageIDBase := sg.generateMessageID(msgType)
		partNum := 1
		totalRows := 0

		currentPartRows := [][]string{}
		currentSize := 0
		overheadSize := 5000

		for {
			select {
			case <-ctx.Done():
				partsChan <- &PartResult{
					Error: ctx.Err(),
				}
				// Отправляем summary даже при отмене контекста
				summaryChan <- &StreamingSummary{
					MessageIDBase: messageIDBase,
					TotalParts:    partNum - 1, // Уже созданные части
					TotalRows:     totalRows,
				}
				return

			case row, ok := <-rowsChan:
				if !ok {
					// Канал закрыт, генерируем последнюю часть
					if len(currentPartRows) > 0 {
						packet, err := sg.createPartWithSender(
							messageIDBase,
							partNum,
							0,
							tableName,
							msgType,
							inReplyTo,
							sender,
							recipient,
							schema,
							currentPartRows,
						)

						partsChan <- &PartResult{
							Packet:    packet,
							PartNum:   partNum,
							RowsCount: len(currentPartRows),
							Error:     err,
						}

						totalRows += len(currentPartRows)
					}

					summaryChan <- &StreamingSummary{
						MessageIDBase: messageIDBase,
						TotalParts:    partNum,
						TotalRows:     totalRows,
					}
					return
				}

				rowSize := sg.estimateRowSize(row)

				if currentSize+rowSize+overheadSize > sg.partSizeBytes && len(currentPartRows) > 0 {
					packet, err := sg.createPartWithSender(
						messageIDBase,
						partNum,
						0,
						tableName,
						msgType,
						inReplyTo,
						sender,
						recipient,
						schema,
						currentPartRows,
					)

					partsChan <- &PartResult{
						Packet:    packet,
						PartNum:   partNum,
						RowsCount: len(currentPartRows),
						Error:     err,
					}

					if err != nil {
						// При ошибке отправляем summary и завершаем
						summaryChan <- &StreamingSummary{
							MessageIDBase: messageIDBase,
							TotalParts:    partNum, // Части до ошибки
							TotalRows:     totalRows,
						}
						return
					}

					totalRows += len(currentPartRows)
					partNum++
					currentPartRows = [][]string{}
					currentSize = 0
				}

				currentPartRows = append(currentPartRows, row)
				currentSize += rowSize
			}
		}
	}()

	return partsChan, summaryChan
}

// createPartWithSender создает DataPacket с sender/recipient
func (sg *StreamingGenerator) createPartWithSender(
	messageIDBase string,
	partNum int,
	totalParts int,
	tableName string,
	msgType MessageType,
	inReplyTo string,
	sender string,
	recipient string,
	schema Schema,
	rows [][]string,
) (*DataPacket, error) {
	packet, err := sg.createPart(messageIDBase, partNum, totalParts, tableName, msgType, schema, rows)
	if err != nil {
		return nil, err
	}

	packet.Header.InReplyTo = inReplyTo
	packet.Header.Sender = sender
	packet.Header.Recipient = recipient

	return packet, nil
}

// UpdatePartTotalParts обновляет TotalParts во всех частях (для batch post-processing)
// Используется когда нужно обновить файлы после завершения streaming экспорта
func UpdatePartTotalParts(packets []*DataPacket, totalParts int) {
	for _, packet := range packets {
		packet.Header.TotalParts = totalParts
	}
}

// rowsToData преобразует [][]string в Data (дублирует из generator.go для инкапсуляции)
func (sg *StreamingGenerator) rowsToData(rows [][]string) Data {
	data := Data{
		Rows: make([]Row, len(rows)),
	}

	for i, row := range rows {
		escapedValues := make([]string, len(row))
		for j, value := range row {
			escapedValues[j] = escapeValue(value)
		}
		data.Rows[i] = Row{
			Value: strings.Join(escapedValues, "|"),
		}
	}

	return data
}

// estimateRowSize примерно оценивает размер строки (дублирует из generator.go)
func (sg *StreamingGenerator) estimateRowSize(row []string) int {
	size := 0
	for _, value := range row {
		size += len(value) + 1
	}
	size += 10
	return size * 2
}
