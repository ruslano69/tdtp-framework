# Filter Tooltip Feature

## Описание
При наведении на иконки условий фильтрации (&, ^) в визуальном дизайнере теперь отображается детальная информация о настроенных условиях.

## Примеры tooltip

### Фильтр с оператором AND (&)
```
price ≥ 100 (&)
Click to edit filter
```

### Фильтр с оператором OR (^)
```
status = active (^)
Click to edit filter
```

### Фильтр BETWEEN
```
age BETWEEN 18 AND 65 (&)
Click to edit filter
```

### Фильтр + Сортировка
```
quantity < 50 (&)
Sort: ASC
Click to edit filter & sort
```

### Только сортировка (*)
```
Sort: DESC
Click to edit sort
```

## Реализация

### Функция форматирования
`formatFilterTooltip(filter, fieldName)` преобразует объект фильтра в читаемую строку:

**Вход:**
```javascript
{
  operator: '>=',
  value: '100',
  logic: 'AND'
}
```

**Выход:**
```
price ≥ 100 (&)
```

### Поддерживаемые операторы
- `=` → `=`
- `<>` → `≠`
- `>=` → `≥`
- `<=` → `≤`
- `>` → `>`
- `<` → `<`
- `BW` → `BETWEEN`

### Логические операторы
- `AND` → `&`
- `OR` → `^`

## Использование

1. Откройте Visual Designer (шаг 3)
2. Добавьте таблицу на канвас
3. Кликните на иконку фильтра рядом с полем
4. Настройте условие фильтрации
5. Наведите мышь на иконку (&, ^) чтобы увидеть условие

## Код

### Местоположение
`cmd/tdtp-xray/frontend/src/scripts/wizard.js`

### Функция
```javascript
function formatFilterTooltip(filter, fieldName) {
    if (!filter) return '';

    const operatorMap = {
        '=': '=',
        '<>': '≠',
        '>=': '≥',
        '<=': '≤',
        '>': '>',
        '<': '<',
        'BW': 'BETWEEN'
    };

    const op = operatorMap[filter.operator] || filter.operator;
    const logic = filter.logic === 'OR' ? '^' : '&';

    let condition = '';
    if (filter.operator === 'BW' && filter.value2) {
        condition = `${fieldName} ${op} ${filter.value} AND ${filter.value2}`;
    } else {
        condition = `${fieldName} ${op} ${filter.value}`;
    }

    return `${condition} (${logic})`;
}
```

### Интеграция
```javascript
// Build detailed filter tooltip
let filterTooltip = '';
if (hasFilter && hasSort) {
    filterTooltip = `${formatFilterTooltip(field.filter, field.name)}\\nSort: ${field.sort}\\nClick to edit filter & sort`;
} else if (hasFilter) {
    filterTooltip = `${formatFilterTooltip(field.filter, field.name)}\\nClick to edit filter`;
} else if (hasSort) {
    filterTooltip = `Sort: ${field.sort}\\nClick to edit sort`;
} else {
    filterTooltip = 'Add filter / sort';
}
```
