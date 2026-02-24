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
    }
}
