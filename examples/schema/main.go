package main

import (
	"fmt"
	"log"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

func main() {
	fmt.Println("=== TDTP Schema Module Example ===")

	// 1. Создание схемы через Builder
	fmt.Println("=== Building Schema ===")
	builder := schema.NewBuilder()

	schemaObj := builder.
		AddInteger("ClientID", true).
		AddText("ClientName", 200).
		AddText("INN", 12).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		AddDate("RegistrationDate").
		AddTimestamp("CreatedAt").
		Build()

	fmt.Printf("Created schema with %d fields\n", len(schemaObj.Fields))
	for i, field := range schemaObj.Fields {
		fmt.Printf("  %d. %s (%s)", i+1, field.Name, field.Type)
		if field.Key {
			fmt.Print(" [PRIMARY KEY]")
		}
		fmt.Println()
	}
	fmt.Println()

	// 2. Валидация схемы
	fmt.Println("=== Validating Schema ===")
	validator := schema.NewValidator()

	if err := validator.ValidateSchema(schemaObj); err != nil {
		log.Fatalf("Schema validation failed: %v", err)
	}
	fmt.Println("✓ Schema is valid")
	fmt.Println()

	// 3. Подготовка тестовых данных
	fmt.Println("=== Preparing Test Data ===")
	rows := [][]string{
		{"1001", "ООО Рога и Копыта", "7701234567", "150000.50", "1", "2020-01-15", "2020-01-15T09:00:00Z"},
		{"1002", "ИП Петров", "7702345678", "-5000.00", "1", "2021-03-20", "2021-03-20T10:30:00Z"},
		{"1003", "ЗАО Альфа", "7703456789", "250000.00", "0", "2019-11-10", "2019-11-10T14:15:00Z"},
	}

	fmt.Printf("Prepared %d rows\n\n", len(rows))

	// 4. Валидация данных
	fmt.Println("=== Validating Rows ===")
	errors := validator.ValidateRows(rows, schemaObj)
	if len(errors) > 0 {
		fmt.Println("Validation errors found:")
		for _, err := range errors {
			fmt.Printf("  ✗ %v\n", err)
		}
	} else {
		fmt.Println("✓ All rows are valid")
	}
	fmt.Println()

	// 5. Проверка первичных ключей
	fmt.Println("=== Validating Primary Keys ===")
	if err := validator.ValidatePrimaryKey(rows, schemaObj); err != nil {
		log.Fatalf("Primary key validation failed: %v", err)
	}
	fmt.Println("✓ Primary keys are unique")
	fmt.Println()

	// 6. Парсинг и конвертация значений
	fmt.Println("=== Parsing and Converting Values ===")
	converter := schema.NewConverter()

	// Парсим первую строку
	fmt.Println("Parsing row 1:")
	for i, value := range rows[0] {
		field := schemaObj.Fields[i]
		fieldDef := schema.FieldDef{
			Name:      field.Name,
			Type:      schema.DataType(field.Type),
			Length:    field.Length,
			Precision: field.Precision,
			Scale:     field.Scale,
			Timezone:  field.Timezone,
			Key:       field.Key,
			Nullable:  true,
		}

		tv, err := converter.ParseValue(value, fieldDef)
		if err != nil {
			log.Fatalf("Failed to parse %s: %v", field.Name, err)
		}

		fmt.Printf("  %s: ", field.Name)
		if tv.IsNull {
			fmt.Println("NULL")
		} else {
			switch schema.NormalizeType(tv.Type) {
			case schema.TypeInteger:
				fmt.Printf("%d (integer)\n", *tv.IntValue)
			case schema.TypeReal, schema.TypeDecimal:
				fmt.Printf("%.2f (decimal)\n", *tv.FloatValue)
			case schema.TypeText:
				fmt.Printf("'%s' (text)\n", *tv.StringValue)
			case schema.TypeBoolean:
				fmt.Printf("%v (boolean)\n", *tv.BoolValue)
			case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
				fmt.Printf("%s (time)\n", tv.TimeValue.Format("2006-01-02 15:04:05"))
			}
		}
	}
	fmt.Println()

	// 7. Форматирование обратно в строку
	fmt.Println("=== Formatting Values Back ===")
	intVal := int64(9999)
	tv := &schema.TypedValue{
		Type:     schema.TypeInteger,
		IntValue: &intVal,
	}
	formatted := converter.FormatValue(tv)
	fmt.Printf("Integer 9999 -> '%s'\n", formatted)

	boolVal := true
	tv = &schema.TypedValue{
		Type:      schema.TypeBoolean,
		BoolValue: &boolVal,
	}
	formatted = converter.FormatValue(tv)
	fmt.Printf("Boolean true -> '%s'\n", formatted)

	strVal := "Test|With|Pipes"
	tv = &schema.TypedValue{
		Type:        schema.TypeText,
		StringValue: &strVal,
	}
	formatted = converter.FormatValue(tv)
	fmt.Printf("Text 'Test|With|Pipes' -> '%s'\n", formatted)
	fmt.Println()

	// 8. Создание DataPacket с валидацией
	fmt.Println("=== Creating and Validating DataPacket ===")
	generator := packet.NewGenerator()

	packets, err := generator.GenerateReference("CustTable", schemaObj, rows)
	if err != nil {
		log.Fatalf("Failed to generate packet: %v", err)
	}

	// Валидация пакета
	if err := validator.ValidateDataPacket(packets[0]); err != nil {
		log.Fatalf("Packet validation failed: %v", err)
	}
	fmt.Println("✓ DataPacket is valid")
	fmt.Println()

	// 9. Тест с невалидными данными
	fmt.Println("=== Testing Invalid Data ===")
	invalidRows := [][]string{
		{"not_a_number", "Company", "123", "100.00", "1", "2020-01-01", "2020-01-01T00:00:00Z"},
		{"2", "Company", "123", "100.123", "1", "2020-01-01", "2020-01-01T00:00:00Z"}, // invalid scale
		{"3", "Company", "123", "100.00", "2", "2020-01-01", "2020-01-01T00:00:00Z"},  // invalid boolean
	}

	fmt.Println("Validating invalid rows:")
	errors = validator.ValidateRows(invalidRows, schemaObj)
	for i, err := range errors {
		fmt.Printf("  %d. %v\n", i+1, err)
	}
	fmt.Println()

	// 10. Получение информации о схеме
	fmt.Println("=== Schema Information ===")
	keyFields := validator.GetKeyFields(schemaObj)
	fmt.Printf("Primary key fields: ")
	for i, field := range keyFields {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(field.Name)
	}
	fmt.Println()

	balanceField, err := validator.GetFieldByName(schemaObj, "Balance")
	if err != nil {
		log.Fatalf("Field not found: %v", err)
	}
	fmt.Printf("Balance field: %s (precision=%d, scale=%d)\n",
		balanceField.Type, balanceField.Precision, balanceField.Scale)

	fmt.Println("\n=== Done ===")
}
