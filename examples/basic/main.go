package main

import (
	"fmt"
	"log"

	"github.com/queuebridge/tdtp/pkg/core/packet"
)

func main() {
	fmt.Println("=== TDTP Framework - Packet Example ===")

	// 1. Создание схемы
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ClientID", Type: "INTEGER", Key: true},
			{Name: "ClientName", Type: "TEXT", Length: 200},
			{Name: "INN", Type: "TEXT", Length: 12},
			{Name: "Balance", Type: "DECIMAL", Precision: 18, Scale: 2},
			{Name: "IsActive", Type: "BOOLEAN"},
		},
	}

	// 2. Подготовка данных
	rows := [][]string{
		{"1001", "ООО Рога и Копыта", "7701234567", "150000.50", "1"},
		{"1002", "ИП Петров", "7702345678", "-5000.00", "1"},
		{"1003", "ЗАО Альфа", "7703456789", "250000.00", "0"},
	}

	// 3. Генерация Reference пакета
	generator := packet.NewGenerator()
	
	packets, err := generator.GenerateReference("CustTable", schema, rows)
	if err != nil {
		log.Fatalf("Failed to generate reference: %v", err)
	}

	fmt.Printf("Generated %d packet(s)\n\n", len(packets))

	// 4. Сохранение в файл
	for i, pkt := range packets {
		filename := fmt.Sprintf("/tmp/reference_part_%d.xml", i+1)
		if err := generator.WriteToFile(pkt, filename); err != nil {
			log.Fatalf("Failed to write file: %v", err)
		}
		fmt.Printf("Saved: %s\n", filename)
		fmt.Printf("  MessageID: %s\n", pkt.Header.MessageID)
		fmt.Printf("  Part: %d/%d\n", pkt.Header.PartNumber, pkt.Header.TotalParts)
		fmt.Printf("  Records: %d\n\n", pkt.Header.RecordsInPart)
	}

	// 5. Парсинг обратно
	fmt.Println("=== Parsing back ===")
	parser := packet.NewParser()
	
	parsedPacket, err := parser.ParseFile("/tmp/reference_part_1.xml")
	if err != nil {
		log.Fatalf("Failed to parse: %v", err)
	}

	fmt.Printf("Parsed packet:\n")
	fmt.Printf("  Type: %s\n", parsedPacket.Header.Type)
	fmt.Printf("  Table: %s\n", parsedPacket.Header.TableName)
	fmt.Printf("  Fields: %d\n", len(parsedPacket.Schema.Fields))
	fmt.Printf("  Rows: %d\n\n", len(parsedPacket.Data.Rows))

	// 6. Извлечение данных
	fmt.Println("=== Data rows ===")
	for i, row := range parsedPacket.Data.Rows {
		values := parser.GetRowValues(row)
		fmt.Printf("Row %d: %v\n", i+1, values)
	}
	fmt.Println()

	// 7. Создание Request с Query
	fmt.Println("=== Creating Request ===")
	query := packet.NewQuery()
	query.Limit = 100
	query.Offset = 0
	
	// Добавляем фильтр
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "IsActive", Operator: "eq", Value: "1"},
				{Field: "Balance", Operator: "gt", Value: "1000"},
			},
		},
	}

	requestPacket, err := generator.GenerateRequest(
		"CustTable",
		query,
		"SystemA",
		"SystemB",
	)
	if err != nil {
		log.Fatalf("Failed to generate request: %v", err)
	}

	if err := generator.WriteToFile(requestPacket, "/tmp/request.xml"); err != nil {
		log.Fatalf("Failed to write request: %v", err)
	}

	fmt.Printf("Request saved: /tmp/request.xml\n")
	fmt.Printf("  MessageID: %s\n", requestPacket.Header.MessageID)
	fmt.Printf("  Sender: %s -> Recipient: %s\n", 
		requestPacket.Header.Sender, 
		requestPacket.Header.Recipient)
	fmt.Printf("  Query Limit: %d, Offset: %d\n\n",
		requestPacket.Query.Limit,
		requestPacket.Query.Offset)

	// 8. Создание Response с QueryContext
	fmt.Println("=== Creating Response ===")
	
	responseRows := [][]string{
		{"1001", "ООО Рога и Копыта", "7701234567", "150000.50", "1"},
	}

	queryContext := &packet.QueryContext{
		OriginalQuery: *query,
		ExecutionResults: packet.ExecutionResults{
			TotalRecordsInTable: 3,
			RecordsAfterFilters: 1,
			RecordsReturned:     1,
			MoreDataAvailable:   false,
		},
	}

	responsePackets, err := generator.GenerateResponse(
		"CustTable",
		requestPacket.Header.MessageID,
		schema,
		responseRows,
		queryContext,
		"SystemB",
		"SystemA",
	)
	if err != nil {
		log.Fatalf("Failed to generate response: %v", err)
	}

	if err := generator.WriteToFile(responsePackets[0], "/tmp/response.xml"); err != nil {
		log.Fatalf("Failed to write response: %v", err)
	}

	fmt.Printf("Response saved: /tmp/response.xml\n")
	fmt.Printf("  MessageID: %s\n", responsePackets[0].Header.MessageID)
	fmt.Printf("  InReplyTo: %s\n", responsePackets[0].Header.InReplyTo)
	fmt.Printf("  Records matched: %d\n", 
		responsePackets[0].QueryContext.ExecutionResults.RecordsAfterFilters)

	fmt.Println("\n=== Done ===")
}
