package schema

import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// Builder помогает строить схемы
type Builder struct {
	fields []packet.Field
}

// NewBuilder создает новый builder
func NewBuilder() *Builder {
	return &Builder{
		fields: []packet.Field{},
	}
}

// AddInteger добавляет INTEGER поле
func (b *Builder) AddInteger(name string, key bool) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeInteger),
		Key:  key,
	})
	return b
}

// AddReal добавляет REAL поле
func (b *Builder) AddReal(name string) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeReal),
	})
	return b
}

// AddDecimal добавляет DECIMAL поле
func (b *Builder) AddDecimal(name string, precision, scale int) *Builder {
	if precision == 0 {
		precision = GetDefaultPrecision()
	}
	if scale == 0 {
		scale = GetDefaultScale()
	}

	b.fields = append(b.fields, packet.Field{
		Name:      name,
		Type:      string(TypeDecimal),
		Precision: precision,
		Scale:     scale,
	})
	return b
}

// AddText добавляет TEXT поле
func (b *Builder) AddText(name string, length int) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name:   name,
		Type:   string(TypeText),
		Length: length,
	})
	return b
}

// AddBoolean добавляет BOOLEAN поле
func (b *Builder) AddBoolean(name string) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeBoolean),
	})
	return b
}

// AddDate добавляет DATE поле
func (b *Builder) AddDate(name string) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeDate),
	})
	return b
}

// AddDatetime добавляет DATETIME поле
func (b *Builder) AddDatetime(name string, timezone string) *Builder {
	if timezone == "" {
		timezone = GetDefaultTimezone()
	}

	b.fields = append(b.fields, packet.Field{
		Name:     name,
		Type:     string(TypeDatetime),
		Timezone: timezone,
	})
	return b
}

// AddTimestamp добавляет TIMESTAMP поле
func (b *Builder) AddTimestamp(name string) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeTimestamp),
	})
	return b
}

// AddBlob добавляет BLOB поле
func (b *Builder) AddBlob(name string) *Builder {
	b.fields = append(b.fields, packet.Field{
		Name: name,
		Type: string(TypeBlob),
	})
	return b
}

// AddField добавляет произвольное поле
func (b *Builder) AddField(field packet.Field) *Builder {
	b.fields = append(b.fields, field)
	return b
}

// Build строит схему
func (b *Builder) Build() packet.Schema {
	return packet.Schema{
		Fields: b.fields,
	}
}

// Reset очищает builder
func (b *Builder) Reset() *Builder {
	b.fields = []packet.Field{}
	return b
}

// FieldCount возвращает количество полей
func (b *Builder) FieldCount() int {
	return len(b.fields)
}

// HasKeyField проверяет наличие первичного ключа
func (b *Builder) HasKeyField() bool {
	for _, field := range b.fields {
		if field.Key {
			return true
		}
	}
	return false
}
