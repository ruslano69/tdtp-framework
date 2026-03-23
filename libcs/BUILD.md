# TdtpAxapta.dll — Инструкция по сборке

## Что получится

`TdtpAxapta.dll` — .NET 3.5 сборка (CLR v2.0), которая оборачивает `libtdtp.dll`
через P/Invoke. Подключается в AX 2009 через .NET Interop без дополнительной
конфигурации CLR.

```
AX 2009 (X++) → TdtpAxapta.dll (.NET 3.5) → libtdtp.dll (Go CGO, cdecl)
```

---

## Требования

| Компонент | Версия | Зачем |
|---|---|---|
| **Windows** | XP и новее | libtdtp.dll — Windows DLL |
| **.NET Framework 3.5 Dev Pack** | 3.5 | Таргет-фреймворк (mscorlib, System) |
| **MSBuild** | 4.0+ | Компиляция .csproj |

MSBuild входит в состав любого из следующих (выбери одно):
- **Visual Studio** 2010 и новее (любой выпуск, включая Community)
- **Visual Studio Build Tools** (бесплатно, без IDE)
  → https://visualstudio.microsoft.com/visual-cpp-build-tools/
- **.NET Framework 4.x SDK** (содержит MSBuild)

> **Примечание:** .NET Framework 3.5 Dev Pack устанавливается через
> *Панель управления → Программы → Включение компонентов Windows →
> .NET Framework 3.5* (на Windows Server включается отдельно через DISM).

---

## Сборка

### Вариант 1: Developer Command Prompt (рекомендуется)

```cmd
rem Открой "Developer Command Prompt for VS" из меню Пуск

cd C:\path\to\tdtp-framework\libcs

msbuild TdtpAxapta.csproj /p:Configuration=Release
```

Результат: `libcs\bin\Release\TdtpAxapta.dll`

---

### Вариант 2: Обычный cmd (если MSBuild не в PATH)

```cmd
rem VS 2022
"C:\Program Files\Microsoft Visual Studio\2022\Community\MSBuild\Current\Bin\MSBuild.exe" ^
    TdtpAxapta.csproj /p:Configuration=Release

rem VS 2019
"C:\Program Files (x86)\Microsoft Visual Studio\2019\BuildTools\MSBuild\Current\Bin\MSBuild.exe" ^
    TdtpAxapta.csproj /p:Configuration=Release
```

---

### Вариант 3: Visual Studio GUI

1. Открой `TdtpAxapta.csproj` в Visual Studio
2. Конфигурация: **Release**, платформа: **Any CPU**
3. `Build → Build Solution` (Ctrl+Shift+B)

---

## Развёртывание на сервере AX 2009

Скопировать оба файла в одну папку:

```
C:\Program Files\Microsoft Dynamics AX\50\Server\<instance>\bin\
├── TdtpAxapta.dll    ← наша сборка
└── libtdtp.dll       ← Go CGO библиотека (собирается из tdtp-framework)
```

Рядом с `Ax32Serv.exe` — оба файла должны быть в одном каталоге или в PATH.

---

## Регистрация в AX 2009

1. AOT → References → правой кнопкой → **Add Reference**
2. Выбрать `TdtpAxapta.dll`
3. Дождаться компиляции CIL

---

## Использование в X++

```xpp
// Чтение TDTP-файла
str dataJson = TdtpAxapta.Tdtp::ReadFile(@"C:\data\export.tdtp.xml");

// Фильтрация: Balance > 1000 AND Status = 'Active'
str filtered = TdtpAxapta.Tdtp::FilterRows(
    dataJson,
    "Balance > 1000 AND Status = 'Active'",
    100  // limit
);

// Запись файла
str result = TdtpAxapta.Tdtp::WriteFile(filtered, @"C:\data\out.tdtp.xml");

// Постраничная навигация (page 2, по 50 строк)
str page2 = TdtpAxapta.Tdtp::FilterRowsPage(dataJson, "Balance > 0", 50, 50);

// Версия библиотеки
str ver = TdtpAxapta.Tdtp::GetVersion();
info(ver);
```

---

## Проверка после сборки

```cmd
rem Убедиться что DLL таргетирует CLR 2.0
ildasm bin\Release\TdtpAxapta.dll /text | findstr "ver 2:"

rem Ожидаемый вывод:
rem   .ver 2:0:0:0
```

---

## Заметки

**Кодировка строк:** `CharSet.Ansi` — строки передаются в кодовой странице ОС
(CP1251 на русском Windows). Для путей с кириллицей это работает корректно.
Значения данных внутри JSON экранированы (`\uXXXX`), поэтому кириллица в данных
передаётся без потерь.

**libtdtp.dll архитектура:** должна совпадать с разрядностью AOS-процесса:
- 32-битный AOS → `libtdtp.dll` x86
- 64-битный AOS → `libtdtp.dll` x64

Проверить разрядность AOS: `Task Manager → Ax32Serv.exe` — наличие `*32` означает 32-бит.
