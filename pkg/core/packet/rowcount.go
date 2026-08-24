package packet

import "fmt"

// VerifyRowCount сверяет RecordsInPart с фактическим числом строк.
//
// Проверка НЕ зависит от версии пакета, и это исправление прежнего поведения.
// Раньше её гейтил NeedsRowCountCheck — то есть для v1.4 и выше она молчала, с
// обоснованием «целостность гарантируется XXH3, счётчик избыточен». Обоснование
// неверно: computeHashes накрывает Schema и значения строк, заголовок в хеш не
// входит вовсе. Пакет v1.4, заявляющий 999 строк при трёх реальных, разбирался
// без замечаний и проходил VerifyIntegrity.
//
// Счётчик читают как авторитетный (etl/processor.go складывает из него
// TotalRowsLoaded, libtdtp отдаёт как число строк), так что расхождение — это
// неверная цифра у потребителя, а не косметика.
//
// Зовётся там, где строки уже в построчном виде: на разборе несжатого пакета и
// сразу после распаковки. На сжатом или зашифрованном пакете возвращает nil —
// сверять нечего, строки ещё в блобе.
func VerifyRowCount(pkt *DataPacket) error {
	if pkt.Header.RecordsInPart <= 0 {
		return nil // счётчик не заявлен
	}
	if pkt.Data.Compression != "" || pkt.Data.Encryption != "" {
		return nil // строки непрозрачны — проверять нечем
	}
	if pkt.Data.Layout == LayoutColumns {
		return nil // <R> здесь колонки; счёт даёт ExpandColumnarRows
	}
	if actual := len(pkt.Data.Rows); actual != pkt.Header.RecordsInPart {
		return fmt.Errorf("RecordsInPart mismatch: header declares %d rows, <Data> contains %d",
			pkt.Header.RecordsInPart, actual)
	}
	return nil
}
