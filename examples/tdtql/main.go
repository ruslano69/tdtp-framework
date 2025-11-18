package main

import (
	"encoding/xml"
	"fmt"
	"log"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/tdtql"
)

func main() {
	fmt.Println("=== TDTP TDTQL Translator Example ===")

	translator := tdtql.NewTranslator()

	// 1. Простой запрос
	fmt.Println("=== Example 1: Simple WHERE ===")
	sql1 := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
	query1, err := translator.Translate(sql1)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql1, query1)

	// 2. IN оператор
	fmt.Println("\n=== Example 2: IN Operator ===")
	sql2 := "SELECT * FROM Users WHERE City IN ('Moscow', 'SPb', 'Kazan')"
	query2, err := translator.Translate(sql2)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql2, query2)

	// 3. BETWEEN
	fmt.Println("\n=== Example 3: BETWEEN ===")
	sql3 := "SELECT * FROM Products WHERE Price BETWEEN 100 AND 1000"
	query3, err := translator.Translate(sql3)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql3, query3)

	// 4. IS NULL
	fmt.Println("\n=== Example 4: IS NULL ===")
	sql4 := "SELECT * FROM Users WHERE DeletedAt IS NULL"
	query4, err := translator.Translate(sql4)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql4, query4)

	// 5. Сложный запрос с OR и скобками
	fmt.Println("\n=== Example 5: Complex Query with OR ===")
	sql5 := `SELECT * FROM CustTable
		WHERE IsActive = 1
		  AND (Balance > 1000 OR Balance < -1000)
		  AND (City = 'Moscow' OR City = 'SPb')
		ORDER BY Balance DESC
		LIMIT 100
		OFFSET 0`
	
	query5, err := translator.Translate(sql5)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql5, query5)

	// 6. LIKE оператор
	fmt.Println("\n=== Example 6: LIKE Operator ===")
	sql6 := "SELECT * FROM Companies WHERE Name LIKE 'ООО%'"
	query6, err := translator.Translate(sql6)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql6, query6)

	// 7. Множественная сортировка
	fmt.Println("\n=== Example 7: Multiple ORDER BY ===")
	sql7 := "SELECT * FROM Users ORDER BY City ASC, Balance DESC, Name ASC"
	query7, err := translator.Translate(sql7)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	printQuery("SQL", sql7, query7)

	// 8. Использование с DataPacket
	fmt.Println("\n=== Example 8: Integration with DataPacket ===")
	
	// Транслируем SQL
	sql8 := "SELECT * FROM TestTable WHERE Status = 'active' ORDER BY CreatedAt DESC LIMIT 50"
	query8, err := translator.Translate(sql8)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Создаем Request пакет
	generator := packet.NewGenerator()
	requestPacket, err := generator.GenerateRequest(
		"TestTable",
		query8,
		"ClientApp",
		"ServerApp",
	)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Сериализуем в XML
	xmlData, err := generator.ToXML(requestPacket, true)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Println("Generated TDTP Request Packet:")
	fmt.Println(string(xmlData))

	// 9. Только WHERE часть
	fmt.Println("\n=== Example 9: Translate WHERE Only ===")
	whereClause := "IsActive = 1 AND Balance > 0 AND Status IN ('new', 'active')"
	filters, err := translator.TranslateWhere(whereClause)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("WHERE Clause: %s\n", whereClause)
	fmt.Println("\nGenerated Filters:")
	if filters.And != nil {
		fmt.Printf("  AND Group with %d filters\n", len(filters.And.Filters))
		for i, f := range filters.And.Filters {
			fmt.Printf("    %d. %s %s %s\n", i+1, f.Field, f.Operator, f.Value)
		}
	}

	fmt.Println("\n=== Done ===")
}

func printQuery(label, sql string, query *packet.Query) {
	fmt.Printf("%s: %s\n", label, sql)
	fmt.Println("\nGenerated TDTQL Query:")
	
	// Сериализуем Query в XML для наглядности
	xmlData, err := xml.MarshalIndent(query, "", "  ")
	if err != nil {
		log.Printf("Error marshaling: %v", err)
		return
	}
	
	fmt.Println(string(xmlData))
}
