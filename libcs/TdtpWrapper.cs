// TdtpWrapper.cs — P/Invoke обёртка для libtdtp.dll
// Цель: .NET 3.5 (CLR 2.0), совместима с AX 2009
//
// Используется JSON API (J_*): все входные и выходные данные — строки JSON.
// Управление памятью: каждая J_* функция возвращает char*, который
// автоматически освобождается через J_FreeString внутри MarshalAndFree.
//
// Развёртывание на сервере AX:
//   1. Скопировать TdtpAxapta.dll в папку рядом с Ax32Serv.exe
//      (или в любую папку из PATH / GAC)
//   2. Скопировать libtdtp.dll туда же
//   3. В AX: через .NET Interop добавить TdtpAxapta.dll и вызывать Tdtp.*

using System;
using System.Runtime.InteropServices;

namespace TdtpAxapta
{
    /// <summary>
    /// Обёртка над JSON API библиотеки libtdtp.dll.
    /// Все методы возвращают JSON-строку. Память освобождается автоматически.
    /// </summary>
    public static class Tdtp
    {
        private const string DllName = "libtdtp.dll";

        // ----------------------------------------------------------------
        // P/Invoke: внутренние объявления
        // CallingConvention.Cdecl — Go CGO экспортирует с cdecl.
        // CharSet.Ansi — строки передаются как char* (ANSI / кодовая страница ОС).
        // ----------------------------------------------------------------

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern void J_FreeString(IntPtr s);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_GetVersion();

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_ReadFile(string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_WriteFile(string dataJSON, string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_ExportAll(string dataJSON, string basePath, string optionsJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_FilterRows(string dataJSON, string whereClause, int limit);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_FilterRowsPage(string dataJSON, string whereClause, int limit, int offset);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_ApplyProcessor(string dataJSON, string procType, string configJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_ApplyChain(string dataJSON, string chainJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Diff(string oldJSON, string newJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_SerializeValue(string tdtpType, string value);

        // --- v1.9.7: инспекция, целостность, трансформации ----------------

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Inspect(string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Test(string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Verify(string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Stamp(string dataJSON, string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_ReadMultipart(string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Sort(string dataJSON, string orderByJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_Merge(string packetsJSON, string optionsJSON);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        private static extern IntPtr J_WriteColumnar(string columnarJSON, string outPath);

        // ----------------------------------------------------------------
        // Вспомогательный метод: IntPtr → string + освободить память Go
        // ----------------------------------------------------------------

        private static string MarshalAndFree(IntPtr ptr)
        {
            if (ptr == IntPtr.Zero)
                return null;
            try
            {
                return Marshal.PtrToStringAnsi(ptr);
            }
            finally
            {
                J_FreeString(ptr);
            }
        }

        // ----------------------------------------------------------------
        // Публичный API
        // ----------------------------------------------------------------

        /// <summary>Версия библиотеки libtdtp.</summary>
        public static string GetVersion()
        {
            return MarshalAndFree(J_GetVersion());
        }

        /// <summary>
        /// Читает TDTP XML-файл и возвращает его содержимое как JSON.
        /// </summary>
        /// <param name="path">Путь к .tdtp.xml файлу</param>
        /// <returns>JSON с полями schema, header, data</returns>
        public static string ReadFile(string path)
        {
            return MarshalAndFree(J_ReadFile(path));
        }

        /// <summary>
        /// Записывает JSON-данные в TDTP XML-файл.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="path">Путь для записи</param>
        /// <returns>{"ok":true} или {"error":"..."}</returns>
        public static string WriteFile(string dataJSON, string path)
        {
            return MarshalAndFree(J_WriteFile(dataJSON, path));
        }

        /// <summary>
        /// Разбивает данные на части и записывает их на диск.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="basePath">Базовый путь, например C:\export\Users.tdtp.xml</param>
        /// <param name="optionsJSON">{"compress":false,"level":3,"checksum":true} (все поля опциональны)</param>
        /// <returns>{"files":[...],"total_parts":N} или {"error":"..."}</returns>
        public static string ExportAll(string dataJSON, string basePath, string optionsJSON)
        {
            return MarshalAndFree(J_ExportAll(dataJSON, basePath, optionsJSON));
        }

        /// <summary>
        /// Фильтрует строки по TDTQL WHERE-условию.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="whereClause">Например: "Balance > 1000 AND City = 'Omsk'"</param>
        /// <param name="limit">Макс. строк (0 = без ограничений)</param>
        /// <returns>JSON с отфильтрованными строками</returns>
        public static string FilterRows(string dataJSON, string whereClause, int limit)
        {
            return MarshalAndFree(J_FilterRows(dataJSON, whereClause, limit));
        }

        /// <summary>
        /// Фильтрует строки с постраничной навигацией.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="whereClause">TDTQL WHERE-условие</param>
        /// <param name="limit">Строк на страницу (0 = без ограничений)</param>
        /// <param name="offset">Пропустить N совпавших строк</param>
        /// <returns>JSON + поле query_context с мета-данными пагинации</returns>
        public static string FilterRowsPage(string dataJSON, string whereClause, int limit, int offset)
        {
            return MarshalAndFree(J_FilterRowsPage(dataJSON, whereClause, limit, offset));
        }

        /// <summary>
        /// Применяет один процессор к данным.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="procType">field_masker | field_normalizer | field_validator | compress | decompress</param>
        /// <param name="configJSON">JSON-конфигурация процессора</param>
        /// <returns>JSON с обработанными данными или {"error":"..."}</returns>
        public static string ApplyProcessor(string dataJSON, string procType, string configJSON)
        {
            return MarshalAndFree(J_ApplyProcessor(dataJSON, procType, configJSON));
        }

        /// <summary>
        /// Применяет цепочку процессоров последовательно.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="chainJSON">[{"type":"field_masker","params":{...}}, ...]</param>
        /// <returns>JSON с обработанными данными или {"error":"..."}</returns>
        public static string ApplyChain(string dataJSON, string chainJSON)
        {
            return MarshalAndFree(J_ApplyChain(dataJSON, chainJSON));
        }

        /// <summary>
        /// Вычисляет разницу между двумя датасетами.
        /// </summary>
        /// <param name="oldJSON">Исходный датасет (JSON)</param>
        /// <param name="newJSON">Новый датасет (JSON)</param>
        /// <returns>{"added":[...],"removed":[...],"modified":[...],"stats":{...}}</returns>
        public static string Diff(string oldJSON, string newJSON)
        {
            return MarshalAndFree(J_Diff(oldJSON, newJSON));
        }

        /// <summary>
        /// Сериализует значение в каноническую wire-форму TDTP.
        /// </summary>
        /// <param name="tdtpType">BLOB | TIMESTAMP | DATETIME | JSON | JSONB | INTEGER | ...</param>
        /// <param name="value">Сырое значение строкой</param>
        /// <returns>{"value":"..."} или {"error":"..."}</returns>
        public static string SerializeValue(string tdtpType, string value)
        {
            return MarshalAndFree(J_SerializeValue(tdtpType, value));
        }

        // ----------------------------------------------------------------
        // v1.9.7: инспекция, целостность, трансформации
        // ----------------------------------------------------------------

        /// <summary>
        /// Возвращает метаданные пакета (схема, заголовок, число строк, сжатие,
        /// compact-флаг) без распаковки секции данных.
        /// </summary>
        /// <param name="path">Путь к .tdtp.xml файлу</param>
        /// <returns>JSON с метаданными или {"error":"..."}</returns>
        public static string Inspect(string path)
        {
            return MarshalAndFree(J_Inspect(path));
        }

        /// <summary>
        /// Dry-run проверка целостности: наличие всех частей, контрольная сумма,
        /// распаковка и подсчёт строк. Файл не импортируется.
        /// </summary>
        /// <param name="path">Путь к .tdtp.xml файлу (или одной из частей)</param>
        /// <returns>{"ok":true,"total_parts":N,"total_rows":M,...} или {"error":"..."}</returns>
        public static string Test(string path)
        {
            return MarshalAndFree(J_Test(path));
        }

        /// <summary>
        /// Проверяет XXH3-хеши целостности v1.4 локально (без Mercury).
        /// </summary>
        /// <param name="path">Путь к подписанному .tdtp.xml файлу</param>
        /// <returns>{"ok":bool,"has_integrity":bool,"packet_xxh3":"...","detail":"..."}</returns>
        public static string Verify(string path)
        {
            return MarshalAndFree(J_Verify(path));
        }

        /// <summary>
        /// Вычисляет XXH3-хеши и записывает подписанный файл v1.4.
        /// Продьюсер-сторона <see cref="Verify"/>.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="path">Путь для записи подписанного файла</param>
        /// <returns>{"ok":true,"path":"...","packet_xxh3":"...","schema_xxh3":"...","data_xxh3":"..."}</returns>
        public static string Stamp(string dataJSON, string path)
        {
            return MarshalAndFree(J_Stamp(dataJSON, path));
        }

        /// <summary>
        /// Собирает многочастный набор (_part_N_of_M) в один датасет.
        /// Можно передать путь к любой из частей.
        /// </summary>
        /// <param name="path">Путь к одной из частей набора</param>
        /// <returns>JSON со склеенным датасетом или {"error":"..."}</returns>
        public static string ReadMultipart(string path)
        {
            return MarshalAndFree(J_ReadMultipart(path));
        }

        /// <summary>
        /// Сортирует строки по одному или нескольким полям.
        /// </summary>
        /// <param name="dataJSON">JSON (schema + header + data)</param>
        /// <param name="orderByJSON">
        /// Имя поля строкой ("Balance") или массив ключей
        /// (например [{"field":"City","direction":"ASC"},{"field":"Balance","direction":"DESC"}]).
        /// </param>
        /// <returns>JSON с отсортированными строками или {"error":"..."}</returns>
        public static string Sort(string dataJSON, string orderByJSON)
        {
            return MarshalAndFree(J_Sort(dataJSON, orderByJSON));
        }

        /// <summary>
        /// Объединяет несколько датасетов в один.
        /// </summary>
        /// <param name="packetsJSON">JSON-массив датасетов: [{schema,header,data}, ...]</param>
        /// <param name="optionsJSON">
        /// {"strategy":"union|intersection|left|right|append","key_fields":["ID",...]}
        /// (можно передать null / пустую строку — по умолчанию union).
        /// </param>
        /// <returns>JSON со склеенным датасетом и статистикой слияния или {"error":"..."}</returns>
        public static string Merge(string packetsJSON, string optionsJSON)
        {
            return MarshalAndFree(J_Merge(packetsJSON, optionsJSON));
        }

        /// <summary>
        /// Записывает TDTP-файл из column-major данных (по столбцу на массив).
        /// Быстрее построчного <see cref="WriteFile"/> на больших числовых наборах:
        /// транспонирование column→row выполняется внутри Go одной аллокацией.
        /// </summary>
        /// <param name="columnarJSON">
        /// {"schema":{...},"header":{...},"columns":[["1","2","3"],["Alice","Bob","Carol"], ...]}
        /// </param>
        /// <param name="outPath">Путь для записи</param>
        /// <returns>{"ok":true} или {"error":"..."}</returns>
        public static string WriteColumnar(string columnarJSON, string outPath)
        {
            return MarshalAndFree(J_WriteColumnar(columnarJSON, outPath));
        }
    }
}
