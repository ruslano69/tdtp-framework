package main

import (
	"fmt"
	"log"

	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
)

func main() {
	fmt.Println("=== TDTP TDTQL Executor Example ===")

	// 1. Создаем схему
	fmt.Println("=== Creating Schema ===")
	schemaObj := schema.NewBuilder().
		AddInteger("ClientID", true).
		AddText("ClientName", 200).
		AddText("City", 100).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		Build()

	fmt.Printf("Schema created with %d fields\n\n", len(schemaObj.Fields))

	// 2. Подготавливаем тестовые данные
	fmt.Println("=== Test Data ===")
	rows := [][]string{
		{"1001", "ООО Рога и Копыта", "Moscow", "150000.50", "1"},
		{"1002", "ИП Петров", "SPb", "-5000.00", "1"},
		{"1003", "ЗАО Альфа", "Moscow", "250000.00", "0"},
		{"1004", "ООО Бета", "Kazan", "75000.00", "1"},
		{"1005", "ИП Сидоров", "SPb", "125000.00", "1"},
		{"1006", "ООО Гамма", "Moscow", "-15000.00", "1"},
		{"1007", "ЗАО Дельта", "Novosibirsk", "50000.00", "0"},
		{"1008", "ООО Эпсилон", "Moscow", "300000.00", "1"},
	}

	fmt.Printf("Total rows: %d\n\n", len(rows))

	// 3. Полный цикл: SQL → TDTQL → Execute
	fmt.Println("=== Example 1: Full SQL → Execute Cycle ===")
	
	sql := `SELECT * FROM CustTable
		WHERE IsActive = 1 
		  AND Balance > 50000
		  AND (City = 'Moscow' OR City = 'SPb')
		ORDER BY Balance DESC
		LIMIT 3`

	fmt.Printf("SQL: %s\n\n", sql)

	// Транслируем SQL в TDTQL
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		log.Fatalf("Translation error: %v", err)
	}

	// Выполняем запрос
	executor := tdtql.NewExecutor()
	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("SQL Query Result", result)

	// 4. Простая фильтрация
	fmt.Println("\n=== Example 2: Simple Filter (Balance > 100000) ===")
	
	query2 := packet.NewQuery()
	query2.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Balance", Operator: "gt", Value: "100000"},
			},
		},
	}

	result2, err := executor.Execute(query2, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("Simple Filter", result2)

	// 5. IN оператор
	fmt.Println("\n=== Example 3: IN Operator ===")
	
	query3 := packet.NewQuery()
	query3.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "City", Operator: "in", Value: "Moscow,SPb,Kazan"},
			},
		},
	}

	result3, err := executor.Execute(query3, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("IN Operator", result3)

	// 6. Сортировка
	fmt.Println("\n=== Example 4: Sorting by Balance DESC ===")
	
	query4 := packet.NewQuery()
	query4.OrderBy = &packet.OrderBy{
		Field:     "Balance",
		Direction: "DESC",
	}
	query4.Limit = 5

	result4, err := executor.Execute(query4, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("Sorted Data", result4)

	// 7. Пагинация
	fmt.Println("\n=== Example 5: Pagination (LIMIT 3 OFFSET 2) ===")
	
	query5 := packet.NewQuery()
	query5.Limit = 3
	query5.Offset = 2

	result5, err := executor.Execute(query5, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("Paginated Data", result5)

	// 8. Комплексный запрос с QueryContext
	fmt.Println("\n=== Example 6: Complex Query with QueryContext ===")
	
	sql6 := `SELECT * FROM CustTable
		WHERE IsActive = 1
		  AND (Balance > 100000 OR Balance < 0)
		ORDER BY Balance DESC
		LIMIT 10 OFFSET 0`

	query6, _ := translator.Translate(sql6)
	result6, err := executor.Execute(query6, rows, schemaObj)
	if err != nil {
		log.Fatalf("Execution error: %v", err)
	}

	printResult("Complex Query", result6)

	// Показываем QueryContext
	fmt.Println("\n=== QueryContext (Stateless Pattern) ===")
	if result6.QueryContext != nil {
		qc := result6.QueryContext
		fmt.Printf("Total Records in Table: %d\n", qc.ExecutionResults.TotalRecordsInTable)
		fmt.Printf("Records After Filters: %d\n", qc.ExecutionResults.RecordsAfterFilters)
		fmt.Printf("Records Returned: %d\n", qc.ExecutionResults.RecordsReturned)
		fmt.Printf("More Data Available: %v\n", qc.ExecutionResults.MoreDataAvailable)
		if qc.ExecutionResults.MoreDataAvailable {
			fmt.Printf("Next Offset: %d\n", qc.ExecutionResults.NextOffset)
		}
	}

	// 9. Создание Response пакета с результатами
	fmt.Println("\n=== Example 7: Creating Response Packet ===")
	
	generator := packet.NewGenerator()
	responsePackets, err := generator.GenerateResponse(
		"CustTable",
		"REQ-2025-001",
		schemaObj,
		result6.FilteredRows,
		result6.QueryContext,
		"Server",
		"Client",
	)
	if err != nil {
		log.Fatalf("Response generation error: %v", err)
	}

	fmt.Printf("Generated %d response packet(s)\n", len(responsePackets))
	fmt.Printf("MessageID: %s\n", responsePackets[0].Header.MessageID)
	fmt.Printf("InReplyTo: %s\n", responsePackets[0].Header.InReplyTo)
	fmt.Printf("Records in packet: %d\n", responsePackets[0].Header.RecordsInPart)

	fmt.Println("\n=== Done ===")
}

func printResult(title string, result *tdtql.ExecutionResult) {
	fmt.Printf("--- %s ---\n", title)
	fmt.Printf("Total Rows: %d\n", result.TotalRows)
	fmt.Printf("Matched Rows: %d\n", result.MatchedRows)
	fmt.Printf("Returned Rows: %d\n", result.ReturnedRows)
	
	if result.MoreAvailable {
		fmt.Printf("More Available: YES (next offset: %d)\n", result.NextOffset)
	} else {
		fmt.Printf("More Available: NO\n")
	}

	fmt.Println("\nFiltered Data:")
	for i, row := range result.FilteredRows {
		fmt.Printf("  %d. ID=%s, Name=%s, City=%s, Balance=%s, Active=%s\n",
			i+1, row[0], row[1], row[2], row[3], row[4])
		if i >= 4 { // показываем только первые 5
			fmt.Printf("  ... and %d more\n", len(result.FilteredRows)-5)
			break
		}
	}
}
