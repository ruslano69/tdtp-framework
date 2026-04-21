//go:build !nokafka

package etl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	kafka "github.com/segmentio/kafka-go"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ─── Константы по умолчанию ───────────────────────────────────────────────────

const (
	defaultPacketKB    = 750 // ~750 KB несжатого XML → после zstd level 3 ≈ 100-250 KB → влезает в 1 MB Kafka default
	defaultBatchSend   = 10  // файлов на один SendBatch
	defaultCompressLvl = 3   // zstd уровень
	spoolSubdir        = "tdtp-kafka-spool"
)

// ─── KafkaSpoolExporter ───────────────────────────────────────────────────────

// KafkaSpoolExporter реализует pipeline:
//
//	Writer:  DataPacket → XML → zstd → файл в spoolDir/
//	Sender:  файл из spoolDir/ → kafka.Writer.WriteMessages → delete
//
// Два работника запускаются параллельно и связаны каналом путей файлов.
// Размер каждого сообщения ≤ defaultPacketKB после сжатия — работает
// с любым Kafka-брокером без изменения конфигурации брокера.
//
// Использует kafka.Writer напрямую (без Reader) — без лишнего overhead
// на создание consumer при закрытии соединения.
type KafkaSpoolExporter struct {
	cfg      *KafkaOutputConfig
	spoolDir string        // рабочая директория
	encoder  *zstd.Encoder // переиспользуемый энкодер (EncodeAll потокобезопасен)
	gen      *packet.Generator
	writer   *kafka.Writer // write-only Kafka соединение (без Reader)
}

// NewKafkaSpoolExporter создаёт экспортер, применяя дефолты.
// spoolDir создаётся автоматически; при permanent=false он удаляется после экспорта.
func NewKafkaSpoolExporter(cfg *KafkaOutputConfig, jobID string) (*KafkaSpoolExporter, error) {
	// Применяем дефолты
	if cfg.PacketKB <= 0 {
		cfg.PacketKB = defaultPacketKB
	}
	if cfg.BatchSend <= 0 {
		cfg.BatchSend = defaultBatchSend
	}
	if cfg.CompressAlgo == "" {
		cfg.CompressAlgo = "zstd"
	}
	if cfg.CompressLevel <= 0 {
		cfg.CompressLevel = defaultCompressLvl
	}

	// Выбираем директорию для spool
	base := cfg.SpoolDir
	if base == "" {
		base = filepath.Join(os.TempDir(), spoolSubdir)
	}
	spoolDir := filepath.Join(base, jobID)
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create spool dir %s: %w", spoolDir, err)
	}

	// Создаём zstd энкодер (EncodeAll — потокобезопасен)
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(cfg.CompressLevel)),
		zstd.WithEncoderConcurrency(1), // один поток — вызывающий сам параллелит
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	// Создаём writer-only Kafka соединение.
	// Не создаём Reader — он нам не нужен и его Close() блокирует на несколько секунд.
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
		Compression:  kafka.Snappy,
		MaxAttempts:  3,
		WriteTimeout: 30 * time.Second,
		BatchBytes:   100 * 1024 * 1024, // 100MB — вся партия влезает в один WriteMessages
		BatchTimeout: 5 * time.Millisecond,
	}

	return &KafkaSpoolExporter{
		cfg:      cfg,
		spoolDir: spoolDir,
		encoder:  enc,
		gen:      packet.NewGenerator(),
		writer:   writer,
	}, nil
}

// Close освобождает ресурсы энкодера и Kafka writer.
func (ke *KafkaSpoolExporter) Close() {
	if ke.writer != nil {
		_ = ke.writer.Close()
	}
	_ = ke.encoder.Close()
}

// Cleanup удаляет spool-директорию (вызывать после успешного экспорта).
func (ke *KafkaSpoolExporter) Cleanup() error {
	return os.RemoveAll(ke.spoolDir)
}

// SpoolDir возвращает путь к рабочей директории (для логов / ручного retry).
func (ke *KafkaSpoolExporter) SpoolDir() string { return ke.spoolDir }

// ─── Export ───────────────────────────────────────────────────────────────────

// ExportPackets принимает слайс пакетов и отправляет их в Kafka через spool.
//
// Pipeline:
//
//	Writer goroutine:  packet[i] → XML → zstd → spool/000001.tdtp.zst → fileCh
//	Sender goroutine:  fileCh → batch N файлов → WriteMessages → delete
func (ke *KafkaSpoolExporter) ExportPackets(ctx context.Context, packets []*packet.DataPacket) error {
	if len(packets) == 0 {
		return nil
	}

	// Быстрый путь: in-memory с ограничением памяти
	if ke.cfg.MemLimitMB > 0 {
		return ke.exportInMemory(ctx, packets)
	}

	// Канал путей к готовым файлам; буфер = BatchSend * 2 чтобы writer не ждал sender
	fileCh := make(chan string, ke.cfg.BatchSend*2)

	var writerErr, senderErr error
	var wg sync.WaitGroup

	// ── Sender goroutine ─────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		senderErr = ke.runSender(ctx, fileCh)
	}()

	// ── Writer (текущая горутина) ─────────────────────────────────────────────
	var seq atomic.Int64
writeLoop:
	for _, pkt := range packets {
		if ctx.Err() != nil {
			writerErr = ctx.Err()
			break
		}

		n := seq.Add(1)
		path := filepath.Join(ke.spoolDir, fmt.Sprintf("%06d.tdtp.zst", n))

		if err := ke.writePacket(pkt, path); err != nil {
			writerErr = fmt.Errorf("packet %d write: %w", n, err)
			break
		}

		select {
		case fileCh <- path:
		case <-ctx.Done():
			writerErr = ctx.Err()
			break writeLoop
		}
	}
	close(fileCh) // сигнал sender'у: больше файлов не будет

	wg.Wait()

	if writerErr != nil {
		return writerErr
	}
	return senderErr
}

// ─── Writer helper ────────────────────────────────────────────────────────────

// writePacket сериализует пакет в XML, сжимает zstd и записывает в файл.
func (ke *KafkaSpoolExporter) writePacket(pkt *packet.DataPacket, path string) error {
	// 1. Materialize rawRows (fast-path GenerateReference)
	pkt.MaterializeRows()

	// 2. Сериализуем в XML
	xmlData, err := ke.gen.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("ToXML: %w", err)
	}

	// 3. Сжимаем zstd (или пропускаем если algo=none)
	var payload []byte
	if ke.cfg.CompressAlgo != "none" {
		payload = ke.encoder.EncodeAll(xmlData, make([]byte, 0, len(xmlData)/4))
	} else {
		payload = xmlData
	}

	// 4. Пишем на диск
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write spool file: %w", err)
	}

	return nil
}

// ─── In-memory bounded pipeline ──────────────────────────────────────────────

// bytesSemaphore — взвешенный семафор: блокирует Acquire пока
// суммарный объём данных в полёте не снизится ниже лимита.
type bytesSemaphore struct {
	mu      sync.Mutex
	cond    *sync.Cond
	current int64
	limit   int64
}

func newBytesSemaphore(limitBytes int64) *bytesSemaphore {
	s := &bytesSemaphore{limit: limitBytes}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire блокирует вызывающего пока current+n > limit или ctx отменён.
func (s *bytesSemaphore) Acquire(ctx context.Context, n int64) error {
	done := ctx.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.current+n > s.limit {
		// Проверяем контекст перед ожиданием
		select {
		case <-done:
			return ctx.Err()
		default:
		}
		s.cond.Wait()
		// После пробуждения снова проверяем контекст
		select {
		case <-done:
			return ctx.Err()
		default:
		}
	}
	s.current += n
	return nil
}

// Release освобождает n байт и будит всех ожидающих.
func (s *bytesSemaphore) Release(n int64) {
	s.mu.Lock()
	s.current -= n
	s.mu.Unlock()
	s.cond.Broadcast()
}

// exportInMemory — быстрый путь без диска.
//
// Writer сжимает пакеты и отправляет []byte в канал.
// Семафор ограничивает суммарный объём сжатых байт в полёте ≤ MemLimitMB.
// Sender батчит и отправляет в Kafka, после чего освобождает семафор.
func (ke *KafkaSpoolExporter) exportInMemory(ctx context.Context, packets []*packet.DataPacket) error {
	if len(packets) == 0 {
		return nil
	}

	sem := newBytesSemaphore(int64(ke.cfg.MemLimitMB) * 1024 * 1024)

	// Буфер 4 слота — writer не ждёт sender между пакетами
	dataCh := make(chan []byte, 4)

	var writerErr, senderErr error
	var wg sync.WaitGroup

	// ── Sender goroutine ─────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		senderErr = ke.runInMemorySender(ctx, dataCh, sem)
	}()

	// ── Writer (текущая горутина) ─────────────────────────────────────────────
	for _, pkt := range packets {
		if ctx.Err() != nil {
			writerErr = ctx.Err()
			break
		}

		pkt.MaterializeRows()
		xmlData, err := ke.gen.ToXML(pkt, true)
		if err != nil {
			writerErr = fmt.Errorf("ToXML: %w", err)
			break
		}

		var payload []byte
		if ke.cfg.CompressAlgo != "none" {
			payload = ke.encoder.EncodeAll(xmlData, make([]byte, 0, len(xmlData)/4))
		} else {
			payload = xmlData
		}

		// Блокируемся если в канале накопилось ≥ MemLimitMB сжатых байт
		if err := sem.Acquire(ctx, int64(len(payload))); err != nil {
			writerErr = err
			break
		}

		select {
		case dataCh <- payload:
		case <-ctx.Done():
			sem.Release(int64(len(payload)))
			writerErr = ctx.Err()
		}
		if writerErr != nil {
			break
		}
	}
	close(dataCh)

	wg.Wait()

	if writerErr != nil {
		return writerErr
	}
	return senderErr
}

// runInMemorySender читает сжатые блоки из канала, батчит и шлёт в Kafka.
// После отправки батча освобождает семафор для всех сообщений батча.
func (ke *KafkaSpoolExporter) runInMemorySender(ctx context.Context, dataCh <-chan []byte, sem *bytesSemaphore) error {
	type entry struct {
		data []byte
	}
	batch := make([]entry, 0, ke.cfg.BatchSend)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		msgs := make([]kafka.Message, 0, len(batch))
		now := time.Now()
		var released int64
		for i, e := range batch {
			msgs = append(msgs, kafka.Message{
				Key:   []byte(fmt.Sprintf("tdtp-%d-%d", now.UnixNano(), i)),
				Value: e.data,
				Time:  now,
				Headers: []kafka.Header{
					{Key: "content-type", Value: []byte("application/xml+zstd")},
					{Key: "protocol", Value: []byte("tdtp")},
				},
			})
			released += int64(len(e.data))
		}

		if err := ke.writer.WriteMessages(ctx, msgs...); err != nil {
			return fmt.Errorf("WriteMessages (%d msgs): %w", len(msgs), err)
		}

		sem.Release(released)
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case data, ok := <-dataCh:
			if !ok {
				return flush()
			}
			batch = append(batch, entry{data: data})
			if len(batch) >= ke.cfg.BatchSend {
				if err := flush(); err != nil {
					return err
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ─── Sender helper ───────────────────────────────────────────────────────────

// runSender читает пути из канала, накапливает батчи и отправляет через kafka.Writer.
// Успешно отправленные файлы удаляются. При ошибке файлы остаются для ручного retry.
func (ke *KafkaSpoolExporter) runSender(ctx context.Context, fileCh <-chan string) error {
	batch := make([]string, 0, ke.cfg.BatchSend)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		msgs := make([]kafka.Message, 0, len(batch))
		now := time.Now()
		for i, p := range batch {
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read spool file %s: %w", p, err)
			}
			msgs = append(msgs, kafka.Message{
				Key:   []byte(fmt.Sprintf("tdtp-%d-%d", now.UnixNano(), i)), //nolint:gocritic
				Value: data,
				Time:  now,
				Headers: []kafka.Header{
					{Key: "content-type", Value: []byte("application/xml+zstd")},
					{Key: "protocol", Value: []byte("tdtp")},
				},
			})
		}

		if err := ke.writer.WriteMessages(ctx, msgs...); err != nil {
			return fmt.Errorf("WriteMessages (%d msgs): %w", len(msgs), err)
		}

		// Удаляем только после успешной отправки
		for _, p := range batch {
			_ = os.Remove(p)
		}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case path, ok := <-fileCh:
			if !ok {
				return flush()
			}
			batch = append(batch, path)
			if len(batch) >= ke.cfg.BatchSend {
				if err := flush(); err != nil {
					return err
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
