//go:build !nokafka

package etl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ─── Константы по умолчанию ───────────────────────────────────────────────────

const (
	defaultPacketKB    = 750 // ~750 KB несжатого XML → после zstd level 3 ≈ 100-250 KB → влезает в 1 MB Kafka default
	defaultBatchSend   = 10  // файлов на один SendBatch
	defaultCompressLvl = 3   // zstd уровень
	spoolSubdir        = "tdtp-kafka-spool"
)

// ─── KafkaSpoolExporter ───────────────────────────────────────────────────────

// KafkaSpoolExporter реализует pipeline:
//
//	Writer:  DataPacket → XML → zstd → файл в spoolDir/
//	Sender:  файл из spoolDir/ → kafka.Writer.WriteMessages → delete
//
// Два работника запускаются параллельно и связаны каналом путей файлов.
// Размер каждого сообщения ≤ defaultPacketKB после сжатия — работает
// с любым Kafka-брокером без изменения конфигурации брокера.
//
// Отправка идёт через общий brokers.Kafka. Свой kafka.Writer здесь был
// заведён ради write-only соединения без Reader, чей Close() блокировал на
// секунды; в brokers.Kafka это позже решили ленивым созданием Reader, и копия
// стала лишней. Пока она жила, конфигурация writer'а существовала в двух
// экземплярах, и всё, что добавлялось в один — включая проверку
// message.max.bytes, — не действовало на другой.
type KafkaSpoolExporter struct {
	cfg      *KafkaOutputConfig
	spoolDir string        // рабочая директория
	encoder  *zstd.Encoder // переиспользуемый энкодер (EncodeAll потокобезопасен)
	gen      *packet.Generator
	broker   *brokers.Kafka // общий writer, см. brokers.NewKafkaWriter
}

// NewKafkaSpoolExporter создаёт экспортер, применяя дефолты.
// spoolDir создаётся автоматически; при permanent=false он удаляется после экспорта.
func NewKafkaSpoolExporter(cfg *KafkaOutputConfig, jobID string) (*KafkaSpoolExporter, error) {
	// Применяем дефолты
	if cfg.PacketKB <= 0 {
		cfg.PacketKB = defaultPacketKB
	}
	if cfg.BatchSend <= 0 {
		cfg.BatchSend = defaultBatchSend
	}
	if cfg.CompressAlgo == "" {
		cfg.CompressAlgo = "zstd"
	}
	if cfg.CompressLevel <= 0 {
		cfg.CompressLevel = defaultCompressLvl
	}

	// Выбираем директорию для spool
	base := cfg.SpoolDir
	if base == "" {
		base = filepath.Join(os.TempDir(), spoolSubdir)
	}
	spoolDir := filepath.Join(base, jobID)
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create spool dir %s: %w", spoolDir, err)
	}

	// Создаём zstd энкодер (EncodeAll — потокобезопасен)
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(cfg.CompressLevel)),
		zstd.WithEncoderConcurrency(1), // один поток — вызывающий сам параллелит
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	// Write-only соединение через общий brokers.Kafka: Reader создаётся там
	// лениво, поэтому запись за него не платит.
	broker, err := brokers.NewKafka(brokers.Config{
		Type:    "kafka",
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka spool: %w", err)
	}
	// Ленивое соединение: writer подключится при первой записи. Спул может
	// создаваться раньше, чем поднимется брокер, и конструктор не место для
	// сетевой проверки.
	broker.ConnectLazy()

	return &KafkaSpoolExporter{
		cfg:      cfg,
		spoolDir: spoolDir,
		encoder:  enc,
		gen:      packet.NewGenerator(),
		broker:   broker,
	}, nil
}

// Close освобождает ресурсы энкодера и Kafka-соединение.
func (ke *KafkaSpoolExporter) Close() {
	if ke.broker != nil {
		_ = ke.broker.Close()
	}
	_ = ke.encoder.Close()
}

// Cleanup удаляет spool-директорию (вызывать после успешного экспорта).
func (ke *KafkaSpoolExporter) Cleanup() error {
	return os.RemoveAll(ke.spoolDir)
}

// SpoolDir возвращает путь к рабочей директории (для логов / ручного retry).
func (ke *KafkaSpoolExporter) SpoolDir() string { return ke.spoolDir }

// ─── Export ───────────────────────────────────────────────────────────────────

// ExportPackets принимает слайс пакетов и отправляет их в Kafka через spool.
//
// Pipeline:
//
//	Writer goroutine:  packet[i] → XML → zstd → spool/000001.tdtp.zst → fileCh
//	Sender goroutine:  fileCh → batch N файлов → WriteMessages → delete
func (ke *KafkaSpoolExporter) ExportPackets(ctx context.Context, packets []*packet.DataPacket) error {
	if len(packets) == 0 {
		return nil
	}

	// Быстрый путь: in-memory с ограничением памяти
	if ke.cfg.MemLimitMB > 0 {
		return ke.exportInMemory(ctx, packets)
	}

	// Канал путей к готовым файлам; буфер = BatchSend * 2 чтобы writer не ждал sender
	fileCh := make(chan string, ke.cfg.BatchSend*2)

	var writerErr, senderErr error
	var wg sync.WaitGroup

	// ── Sender goroutine ─────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		senderErr = ke.runSender(ctx, fileCh)
	}()

	// ── Writer (текущая горутина) ─────────────────────────────────────────────
	var seq atomic.Int64
writeLoop:
	for _, pkt := range packets {
		if ctx.Err() != nil {
			writerErr = ctx.Err()
			break
		}

		n := seq.Add(1)
		path := filepath.Join(ke.spoolDir, fmt.Sprintf("%06d.tdtp.zst", n))

		if err := ke.writePacket(pkt, path); err != nil {
			writerErr = fmt.Errorf("packet %d write: %w", n, err)
			break
		}

		select {
		case fileCh <- path:
		case <-ctx.Done():
			writerErr = ctx.Err()
			break writeLoop
		}
	}
	close(fileCh) // сигнал sender'у: больше файлов не будет

	wg.Wait()

	if writerErr != nil {
		return writerErr
	}
	return senderErr
}

// ─── Writer helper ────────────────────────────────────────────────────────────

// writePacket сериализует пакет в XML, сжимает zstd и записывает в файл.
func (ke *KafkaSpoolExporter) writePacket(pkt *packet.DataPacket, path string) error {
	// 1. Materialize rawRows (fast-path GenerateReference)
	pkt.MaterializeRows()

	// 2. Сериализуем в XML
	xmlData, err := ke.gen.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("ToXML: %w", err)
	}

	// 3. Сжимаем zstd (или пропускаем если algo=none)
	var payload []byte
	if ke.cfg.CompressAlgo != "none" {
		payload = ke.encoder.EncodeAll(xmlData, make([]byte, 0, len(xmlData)/4))
	} else {
		payload = xmlData
	}

	// 4. Пишем на диск
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write spool file: %w", err)
	}

	return nil
}

// ─── In-memory bounded pipeline ──────────────────────────────────────────────

// bytesSemaphore — взвешенный семафор: блокирует Acquire пока
// суммарный объём данных в полёте не снизится ниже лимита.
type bytesSemaphore struct {
	mu      sync.Mutex
	cond    *sync.Cond
	current int64
	limit   int64
}

func newBytesSemaphore(limitBytes int64) *bytesSemaphore {
	s := &bytesSemaphore{limit: limitBytes}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire блокирует вызывающего пока current+n > limit или ctx отменён.
func (s *bytesSemaphore) Acquire(ctx context.Context, n int64) error {
	done := ctx.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.current+n > s.limit {
		// Проверяем контекст перед ожиданием
		select {
		case <-done:
			return ctx.Err()
		default:
		}
		s.cond.Wait()
		// После пробуждения снова проверяем контекст
		select {
		case <-done:
			return ctx.Err()
		default:
		}
	}
	s.current += n
	return nil
}

// Release освобождает n байт и будит всех ожидающих.
func (s *bytesSemaphore) Release(n int64) {
	s.mu.Lock()
	s.current -= n
	s.mu.Unlock()
	s.cond.Broadcast()
}

// exportInMemory — быстрый путь без диска.
//
// Writer сжимает пакеты и отправляет []byte в канал.
// Семафор ограничивает суммарный объём сжатых байт в полёте ≤ MemLimitMB.
// Sender батчит и отправляет в Kafka, после чего освобождает семафор.
func (ke *KafkaSpoolExporter) exportInMemory(ctx context.Context, packets []*packet.DataPacket) error {
	if len(packets) == 0 {
		return nil
	}

	sem := newBytesSemaphore(int64(ke.cfg.MemLimitMB) * 1024 * 1024)

	// Буфер 4 слота — writer не ждёт sender между пакетами
	dataCh := make(chan []byte, 4)

	var writerErr, senderErr error
	var wg sync.WaitGroup

	// ── Sender goroutine ─────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		senderErr = ke.runInMemorySender(ctx, dataCh, sem)
	}()

	// ── Writer (текущая горутина) ─────────────────────────────────────────────
	for _, pkt := range packets {
		if ctx.Err() != nil {
			writerErr = ctx.Err()
			break
		}

		pkt.MaterializeRows()
		xmlData, err := ke.gen.ToXML(pkt, true)
		if err != nil {
			writerErr = fmt.Errorf("ToXML: %w", err)
			break
		}

		var payload []byte
		if ke.cfg.CompressAlgo != "none" {
			payload = ke.encoder.EncodeAll(xmlData, make([]byte, 0, len(xmlData)/4))
		} else {
			payload = xmlData
		}

		// Блокируемся если в канале накопилось ≥ MemLimitMB сжатых байт
		if err := sem.Acquire(ctx, int64(len(payload))); err != nil {
			writerErr = err
			break
		}

		select {
		case dataCh <- payload:
		case <-ctx.Done():
			sem.Release(int64(len(payload)))
			writerErr = ctx.Err()
		}
		if writerErr != nil {
			break
		}
	}
	close(dataCh)

	wg.Wait()

	if writerErr != nil {
		return writerErr
	}
	return senderErr
}

// runInMemorySender читает сжатые блоки из канала, батчит и шлёт в Kafka.
// После отправки батча освобождает семафор для всех сообщений батча.
func (ke *KafkaSpoolExporter) runInMemorySender(ctx context.Context, dataCh <-chan []byte, sem *bytesSemaphore) error {
	type entry struct {
		data []byte
	}
	batch := make([]entry, 0, ke.cfg.BatchSend)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		// Ключи, время и заголовки проставляет SendBatchAs — здесь только
		// полезная нагрузка, чтобы формат сообщения задавался в одном месте.
		values := make([][]byte, 0, len(batch))
		var released int64
		for _, e := range batch {
			values = append(values, e.data)
			released += int64(len(e.data))
		}

		if err := ke.broker.SendBatchAs(ctx, values, brokers.ContentTypeXMLZstd); err != nil {
			return fmt.Errorf("send batch (%d msgs): %w", len(values), err)
		}

		sem.Release(released)
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case data, ok := <-dataCh:
			if !ok {
				return flush()
			}
			batch = append(batch, entry{data: data})
			if len(batch) >= ke.cfg.BatchSend {
				if err := flush(); err != nil {
					return err
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ─── Sender helper ───────────────────────────────────────────────────────────

// runSender читает пути из канала, накапливает батчи и отправляет через kafka.Writer.
// Успешно отправленные файлы удаляются. При ошибке файлы остаются для ручного retry.
func (ke *KafkaSpoolExporter) runSender(ctx context.Context, fileCh <-chan string) error {
	batch := make([]string, 0, ke.cfg.BatchSend)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		values := make([][]byte, 0, len(batch))
		for _, p := range batch {
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read spool file %s: %w", p, err)
			}
			values = append(values, data)
		}

		if err := ke.broker.SendBatchAs(ctx, values, brokers.ContentTypeXMLZstd); err != nil {
			return fmt.Errorf("send batch (%d msgs): %w", len(values), err)
		}

		// Удаляем только после успешной отправки
		for _, p := range batch {
			_ = os.Remove(p)
		}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case path, ok := <-fileCh:
			if !ok {
				return flush()
			}
			batch = append(batch, path)
			if len(batch) >= ke.cfg.BatchSend {
				if err := flush(); err != nil {
					return err
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
