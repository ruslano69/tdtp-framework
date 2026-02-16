# TDTP X-Ray Sprint Checklist

**Текущий статус:** Phase 2 завершена (Steps 1-3), Phase 3 в разработке (Steps 4-7)

---

## 🎯 SPRINT 1 (P0): Сквозной Wizard Flow
**Цель:** Пользователь может пройти все 7 шагов и получить валидный YAML

### 📋 Step 4: Transform SQL

#### Frontend Tasks
- [ ] **S1.4.1** Создать UI форму для Transform
  - [ ] Input для `result_table` (название результирующей таблицы)
  - [ ] Textarea для SQL запроса с syntax highlighting
  - [ ] Кнопка "Preview SQL" (показать результат трансформации)
  - [ ] Валидация: result_table обязателен, SQL не пустой
  - **Файл:** `cmd/tdtp-xray/frontend/src/scripts/wizard.js`
  - **Функции:** `getStep4HTML()`, `loadStep4Data()`, `saveStep4()`, `validateStep4()`
  - **DoD:** Форма показывается, данные сохраняются, валидация работает

#### Backend Tasks
- [ ] **S1.4.2** Добавить валидацию Transform
  - [ ] Проверка обязательных полей (result_table, sql)
  - [ ] Базовая SQL валидация (не пустой, нет опасных команд)
  - **Файл:** `cmd/tdtp-xray/app.go`
  - **Функции:** `SaveTransform()`, `ValidateTransform()`
  - **DoD:** Ошибки валидации возвращаются в UI

- [ ] **S1.4.3** Реализовать Preview Transform
  - [ ] Новый метод `PreviewTransform(sql string) PreviewResult`
  - [ ] Выполнение SQL на workspace базе
  - [ ] Возврат sample данных (первые 100 строк)
  - **Файл:** `cmd/tdtp-xray/app.go`
  - **DoD:** Preview показывает результат трансформации

---

### 📋 Step 5: Output Configuration

#### Frontend Tasks
- [ ] **S1.5.1** Создать UI для выбора типа Output
  - [ ] Radio buttons: TDTP File / RabbitMQ / Kafka / Database / XLSX
  - [ ] Динамическая форма в зависимости от типа
  - **Файл:** `wizard.js`
  - **Функции:** `getStep5HTML()`, `onOutputTypeChange()`
  - **DoD:** Форма меняется при смене типа

- [ ] **S1.5.2** Формы для каждого типа Output
  - [ ] **TDTP File:** destination path, compression checkbox
  - [ ] **RabbitMQ:** host, port, queue, user, password
  - [ ] **Kafka:** brokers, topic
  - [ ] **Database:** type, DSN, table, strategy (replace/ignore/copy/fail)
  - [ ] **XLSX:** destination path, sheet name
  - **Файл:** `wizard.js`
  - **DoD:** Все поля корректно сохраняются

#### Backend Tasks
- [ ] **S1.5.3** Валидация Output конфигурации
  - [ ] Проверка обязательных полей для каждого типа
  - [ ] Валидация DSN формата
  - [ ] Проверка доступности путей для file output
  - **Файл:** `app.go`
  - **Функции:** `SaveOutput()`, `ValidateOutput()`
  - **DoD:** Ошибки валидации показываются в UI

---

### 📋 Step 6: Settings (Performance, Audit, Error Handling)

#### Frontend Tasks
- [ ] **S1.6.1** UI для Performance настроек
  - [ ] Timeout (секунды), BatchSize (строки)
  - [ ] ParallelSources checkbox
  - [ ] MaxMemoryMB
  - **Файл:** `wizard.js`
  - **DoD:** Форма с разумными defaults

- [ ] **S1.6.2** UI для Audit настроек
  - [ ] Enabled checkbox
  - [ ] LogFile path, LogQueries, LogErrors checkboxes
  - **DoD:** Сохраняется корректно

- [ ] **S1.6.3** UI для Error Handling
  - [ ] Dropdowns: onSourceError/onTransformError/onExportError (continue/fail)
  - [ ] RetryCount, RetryDelaySec
  - **DoD:** Валидация работает

#### Backend Tasks
- [ ] **S1.6.4** Defaults для Settings
  - [ ] Performance: timeout=300, batchSize=1000, maxMemoryMB=512
  - [ ] ErrorHandling: onSourceError=fail, retryCount=3, retryDelay=5
  - **Файл:** `app.go`
  - **Функции:** `GetSettings()` with defaults
  - **DoD:** При первом открытии шага 6 показываются разумные defaults

---

### 📋 Step 7: Review & Generate YAML

#### Frontend Tasks
- [ ] **S1.7.1** Review экран - сводка конфигурации
  - [ ] Показать: Pipeline Name, Sources (количество), Transform SQL preview
  - [ ] Output type и destination
  - [ ] Settings summary
  - **Файл:** `wizard.js`
  - **Функции:** `getStep7HTML()`, `renderConfigSummary()`
  - **DoD:** Все данные показываются корректно

- [ ] **S1.7.2** Кнопки действий
  - [ ] "Generate YAML" → показать modal с YAML preview
  - [ ] "Save to File" → сохранить YAML на диск
  - [ ] "Copy to Clipboard" → скопировать YAML
  - **Файл:** `wizard.js`
  - **DoD:** YAML корректно генерируется и сохраняется

#### Backend Tasks
- [ ] **S1.7.3** Реализовать полный `GenerateYAML()`
  - [ ] Маппинг App state → TDTPConfig
  - [ ] Сериализация в YAML (использовать `gopkg.in/yaml.v3`)
  - [ ] Обработка omitempty полей
  - **Файл:** `app.go`
  - **Функции:** `GenerateYAML()`, `buildTDTPConfig()`
  - **DoD:** YAML совместим с tdtpcli (загружается без ошибок)

- [ ] **S1.7.4** Валидация перед генерацией
  - [ ] Проверка что все обязательные шаги заполнены
  - [ ] Проверка совместимости источников (mock mode vs production)
  - **Файл:** `app.go`
  - **Функции:** `ValidatePipeline()`
  - **DoD:** Понятные ошибки при незаполненных полях

---

### 📋 Round-Trip: Load Configuration

#### Backend Tasks
- [ ] **S1.8.1** Полная реализация `LoadConfigurationFile()`
  - [ ] Загрузка sources → App.sources ✅ (уже есть)
  - [ ] Загрузка transform → App.transform
  - [ ] Загрузка output → App.output
  - [ ] Загрузка settings → App.settings
  - **Файл:** `app.go` (строка 1035)
  - **DoD:** Все секции YAML загружаются в App state

#### Frontend Tasks
- [ ] **S1.8.2** Синхронизация UI после загрузки
  - [ ] После `LoadConfigurationFile()` вызвать refresh для всех шагов
  - [ ] Обновить Step 4-7 данными из бэкенда
  - **Файл:** `wizard.js`
  - **Функции:** `loadConfigurationFile()`, `refreshAllSteps()`
  - **DoD:** После Load все шаги показывают загруженные данные

- [ ] **S1.8.3** Тест Round-Trip
  - [ ] Save YAML → Load → Save снова → diff должен быть минимальным
  - **DoD:** Семантически эквивалентный YAML

---

## 🔧 SPRINT 2 (P1): Стабилизация и Качество
**Цель:** Улучшить UX, добавить тесты, исправить баги

### 📋 Step 3 Polish

- [ ] **S2.1.1** JOIN валидация
  - [ ] Проверка типов полей (нельзя join INT с VARCHAR)
  - [ ] Предупреждение при self-join
  - [ ] Проверка дубликатов JOIN
  - **Файл:** `wizard.js`
  - **Функции:** `validateJoin()`, `startJoin()`
  - **DoD:** Ошибки показываются в properties panel

- [ ] **S2.1.2** Canvas UX улучшения
  - [ ] Auto-layout для перекрывающихся таблиц
  - [ ] Удаление JOIN при удалении таблицы
  - [ ] Сохранение позиций таблиц в конфигурации
  - **DoD:** Canvas более удобный

### 📋 SQL Generation Improvements

- [ ] **S2.2.1** Поддержка WHERE условий в Visual Designer
  - [ ] UI для добавления фильтров по полям
  - [ ] Генерация WHERE clause в SQL
  - **Файл:** `app.go` (GenerateSQL)
  - **DoD:** Фильтры работают в preview

### 📋 Testing & Documentation

- [ ] **S2.3.1** E2E тест сквозного сценария
  - [ ] Создать тестовый YAML
  - [ ] Load → Modify → Save → Compare
  - **Файл:** `cmd/tdtp-xray/app_test.go`
  - **DoD:** Тест проходит на CI

- [ ] **S2.3.2** Обновить документацию
  - [ ] Актуализировать README с фактическим статусом
  - [ ] Добавить screenshots всех шагов
  - [ ] Обновить DEVELOPMENT_LOG
  - **DoD:** Документация соответствует реальности

---

## 📦 SPRINT 3 (P2): Release Candidate
**Цель:** Подготовка к первому релизу

### 📋 Windows Build & Packaging

- [ ] **S3.1.1** Release checklist
  - [ ] Wails build без ошибок
  - [ ] WebView2 runtime requirements
  - [ ] Smoke test: создать pipeline, сгенерировать YAML, запустить в tdtpcli
  - **DoD:** Релизный билд готов

### 📋 Known Limitations Documentation

- [ ] **S3.2.1** Документировать ограничения v0.1
  - [ ] Не поддерживается: incremental sync, data processors (mask/validate/normalize)
  - [ ] Ограничения Visual Designer: нет GROUP BY, UNION, subqueries
  - [ ] Output types: только TDTP file, RabbitMQ, Kafka (database/xlsx в roadmap)
  - **Файл:** `docs/tdtp-xray/LIMITATIONS.md`
  - **DoD:** Пользователи понимают текущие ограничения

---

## 📊 Acceptance Criteria для Sprint 1 (MVP)

### Must Have (блокеры релиза)
1. ✅ **Сквозной flow:** пользователь может пройти все 7 шагов без заглушек
2. ✅ **YAML генерация:** GenerateYAML() возвращает валидный YAML совместимый с tdtpcli
3. ✅ **Round-trip:** Load YAML → Save YAML → diff минимален
4. ✅ **Валидация:** обязательные поля проверяются, ошибки показываются

### Nice to Have (можно в Sprint 2)
- ⭕ Preview Transform (показать результат SQL)
- ⭕ JOIN валидация (типы полей)
- ⭕ WHERE условия в Visual Designer

### Won't Have (в roadmap)
- ❌ Data Processors (mask/validate/normalize) - Phase 4
- ❌ Incremental Sync - Phase 4
- ❌ Database/XLSX output - Phase 3+
- ❌ Advanced transforms (GROUP BY, UNION) - Phase 4

---

## 🎯 Definition of Done для каждой задачи

### Frontend задачи
1. ✅ HTML форма реализована и корректно отображается
2. ✅ Данные сохраняются в App state через Wails bindings
3. ✅ Валидация работает, ошибки показываются пользователю
4. ✅ Navigation (Back/Next) корректно работает с сохранением данных
5. ✅ Code review пройден

### Backend задачи
1. ✅ Go структуры и методы реализованы
2. ✅ Валидация входных данных добавлена
3. ✅ Errors возвращаются с понятными сообщениями
4. ✅ Unit тесты написаны (минимум happy path)
5. ✅ Code review пройден

### Integration задачи
1. ✅ Frontend + Backend работают вместе
2. ✅ Manual testing пройден (smoke test)
3. ✅ Нет console errors в DevTools
4. ✅ YAML генерация валидна (проверено в tdtpcli)

---

## 📅 Оценки времени (ориентировочно)

| Sprint | Задачи | Оценка | Приоритет |
|--------|--------|--------|-----------|
| **Sprint 1** | Steps 4-7 + GenerateYAML + Round-trip | 5-7 дней | P0 (MVP) |
| **Sprint 2** | Стабилизация, тесты, UX polish | 3-4 дня | P1 |
| **Sprint 3** | Release prep, docs, packaging | 2-3 дня | P2 |

**Total:** ~10-14 дней до первого релиза

---

## 🚀 Quick Start для разработчика

### Начать Sprint 1 сейчас:

1. **Первая задача (самая простая):** S1.4.1 - Step 4 UI
   ```javascript
   // В wizard.js найти getStep4HTML() и заменить на:
   function getStep4HTML() {
       return `
       <div class="step-content active">
           <div class="panel">
               <h3>Transform SQL</h3>
               <label>Result Table Name *</label>
               <input type="text" id="resultTable" placeholder="my_result_table">

               <label>SQL Query *</label>
               <textarea id="transformSQL" rows="10" placeholder="SELECT * FROM ..."></textarea>

               <button onclick="previewTransform()">Preview Result</button>
           </div>
       </div>`;
   }
   ```

2. **Следующая задача:** S1.7.3 - GenerateYAML backend
   ```go
   // В app.go найти GenerateYAML() и реализовать:
   func (a *App) GenerateYAML() (string, error) {
       config := TDTPConfig{
           Name: a.pipelineInfo.Name,
           Version: a.pipelineInfo.Version,
           Description: a.pipelineInfo.Description,
           Sources: a.buildSourceConfigs(),
           Transform: a.buildTransformConfig(),
           // ... остальные секции
       }
       return yaml.Marshal(config)
   }
   ```

### Тестирование каждой задачи:
1. `wails dev` → открыть X-Ray
2. Создать pipeline, пройти шаги 1-7
3. Generate YAML → проверить вывод
4. Save YAML → загрузить в tdtpcli: `tdtpcli --pipeline generated.yaml`

---

**Последнее обновление:** 2026-02-16
**Автор:** Claude Code Agent
**Версия:** 1.0
