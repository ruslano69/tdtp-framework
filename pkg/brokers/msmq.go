// +build windows

package brokers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// ============================================================================
// Константы доступа к MSMQ
// ============================================================================

const (
	// MQ_SEND_ACCESS - режим доступа для отправки сообщений
	MQ_SEND_ACCESS = 2
	// MQ_RECEIVE_ACCESS - режим доступа для получения сообщений
	MQ_RECEIVE_ACCESS = 1
	// MQ_PEEK_ACCESS - режим доступа для просмотра без удаления
	MQ_PEEK_ACCESS = 32
	// MQ_DENY_NONE - разделяемый доступ к очереди
	MQ_DENY_NONE = 0
)

// ============================================================================
// Коды ошибок MSMQ (HRESULT)
// ============================================================================

const (
	// MQ_ERROR_QUEUE_EMPTY - очередь пуста
	MQ_ERROR_QUEUE_EMPTY = 0xC00E0002
	// MQ_ERROR_IO_TIMEOUT - истёк таймаут операции
	MQ_ERROR_IO_TIMEOUT = 0xC00E0011
	// E_FAIL - общая ошибка COM
	E_FAIL = 0x80004005
)

// ============================================================================
// MSMQ Broker Implementation
// ============================================================================

// MSMQ реализует MessageBroker для Microsoft Message Queuing (Windows only)
// Оптимизирован для пакетной работы с исправлениями утечек памяти
type MSMQ struct {
	config      Config
	initialized bool
	mu          sync.Mutex

	// Кэш открытых очередей (только для текущей сессии)
	sendQueue    *ole.IDispatch
	receiveQueue *ole.IDispatch
}

// NewMSMQ создаёт новый MSMQ брокер
func NewMSMQ(cfg Config) (*MSMQ, error) {
	if cfg.QueuePath == "" {
		return nil, fmt.Errorf("queue_path is required for MSMQ (example: \".\\\\private$\\\\tdtp_export\")")
	}

	return &MSMQ{
		config:      cfg,
		initialized: false,
	}, nil
}

// Connect инициализирует COM для работы с MSMQ
func (m *MSMQ) Connect(ctx context.Context) error {
	if m.initialized {
		return nil
	}

	// Проверяем контекст
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Инициализируем COM с многопоточным режимом
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err != nil {
		return fmt.Errorf("failed to initialize COM: %w", err)
	}

	m.initialized = true
	return nil
}

// Close закрывает все очереди и деинициализирует COM
func (m *MSMQ) Close() error {
	if !m.initialized {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Закрываем очереди
	if m.sendQueue != nil {
		closeResult, _ := oleutil.CallMethod(m.sendQueue, "Close")
		// ✅ CRITICAL: Free Variant from Close
		if closeResult != nil {
			closeResult.Clear()
		}
		m.sendQueue.Release()
		m.sendQueue = nil
	}

	if m.receiveQueue != nil {
		closeResult, _ := oleutil.CallMethod(m.receiveQueue, "Close")
		// ✅ CRITICAL: Free Variant from Close
		if closeResult != nil {
			closeResult.Clear()
		}
		m.receiveQueue.Release()
		m.receiveQueue = nil
	}

	// Деинициализируем COM
	ole.CoUninitialize()
	m.initialized = false

	return nil
}

// Send отправляет сообщение в MSMQ очередь
func (m *MSMQ) Send(ctx context.Context, message []byte) error {
	if err := m.checkInitialized(); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	queuePath := m.normalizeQueuePath(m.config.QueuePath)

	// Открываем очередь (или используем кэшированную)
	queue, err := m.getOrOpenSendQueue(queuePath)
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}

	// Создаём сообщение
	msg, cleanup, err := m.createMessage()
	if err != nil {
		return err
	}
	defer cleanup()

	// Устанавливаем тело сообщения
	if err := m.setMessageBody(msg, message); err != nil {
		return fmt.Errorf("failed to set message body: %w", err)
	}

	// Устанавливаем персистентность (RECOVERABLE для гарантии доставки)
	result, err := oleutil.PutProperty(msg, "Delivery", int32(1))
	if err == nil && result != nil {
		// ✅ CRITICAL: Free Variant from PutProperty
		result.Clear()
	}

	// Отправляем сообщение
	if err := m.sendMessage(msg, queue); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Receive получает сообщение из MSMQ очереди
func (m *MSMQ) Receive(ctx context.Context) ([]byte, error) {
	if err := m.checkInitialized(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	queuePath := m.normalizeQueuePath(m.config.QueuePath)

	// Открываем очередь (или используем кэшированную)
	queue, err := m.getOrOpenReceiveQueue(queuePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open queue: %w", err)
	}

	// Проверяем контекст перед получением
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Получаем сообщение
	result, err := oleutil.CallMethod(queue, "Receive")
	if err != nil {
		if isQueueEmptyError(err) {
			return nil, nil // Очередь пуста - это нормально
		}
		return nil, fmt.Errorf("failed to receive message: %w", err)
	}

	msgDispatch := result.ToIDispatch()
	if msgDispatch == nil {
		return nil, nil // Нет сообщений
	}

	// ✅ CRITICAL FIX: AddRef() + Clear() паттерн для освобождения Variant!
	// Это ключевое исправление утечки памяти при получении сообщений
	msgDispatch.AddRef()
	result.Clear()
	defer msgDispatch.Release()

	// Получаем тело сообщения
	body, err := m.getMessageBody(msgDispatch)
	if err != nil {
		return nil, fmt.Errorf("failed to get message body: %w", err)
	}

	return body, nil
}

// Ping проверяет доступность MSMQ сервиса
func (m *MSMQ) Ping(ctx context.Context) error {
	if err := m.checkInitialized(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Пробуем создать объект QueueInfo
	unknown, err := oleutil.CreateObject("MSMQ.MSMQQueueInfo")
	if err != nil {
		return fmt.Errorf("MSMQ service not available: %w", err)
	}
	defer unknown.Release()

	return nil
}

// GetBrokerType возвращает тип брокера
func (m *MSMQ) GetBrokerType() string {
	return "msmq"
}

// ============================================================================
// Внутренние методы
// ============================================================================

// checkInitialized проверяет инициализацию
func (m *MSMQ) checkInitialized() error {
	if !m.initialized {
		return fmt.Errorf("MSMQ adapter not initialized, call Connect() first")
	}
	return nil
}

// getOrOpenSendQueue получает или открывает очередь для отправки
func (m *MSMQ) getOrOpenSendQueue(queuePath string) (*ole.IDispatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Если очередь уже открыта, используем её
	if m.sendQueue != nil {
		return m.sendQueue, nil
	}

	// Открываем новую очередь
	queue, err := m.openQueue(queuePath, MQ_SEND_ACCESS)
	if err != nil {
		return nil, err
	}

	m.sendQueue = queue
	return queue, nil
}

// getOrOpenReceiveQueue получает или открывает очередь для получения
func (m *MSMQ) getOrOpenReceiveQueue(queuePath string) (*ole.IDispatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Если очередь уже открыта, используем её
	if m.receiveQueue != nil {
		return m.receiveQueue, nil
	}

	// Открываем новую очередь
	queue, err := m.openQueue(queuePath, MQ_RECEIVE_ACCESS)
	if err != nil {
		return nil, err
	}

	m.receiveQueue = queue
	return queue, nil
}

// openQueue открывает MSMQ очередь
func (m *MSMQ) openQueue(queuePath string, accessMode int) (*ole.IDispatch, error) {
	// Создаём QueueInfo
	unknown, err := oleutil.CreateObject("MSMQ.MSMQQueueInfo")
	if err != nil {
		return nil, fmt.Errorf("failed to create QueueInfo: %w", err)
	}

	queueInfo, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		unknown.Release()
		return nil, fmt.Errorf("failed to query IDispatch: %w", err)
	}
	defer queueInfo.Release()

	// Устанавливаем путь
	pathResult, err := oleutil.PutProperty(queueInfo, "PathName", queuePath)
	if err != nil {
		return nil, fmt.Errorf("failed to set queue path: %w", err)
	}
	// ✅ CRITICAL FIX: Free Variant from PutProperty
	// Вызывается при каждом открытии очереди - критично для предотвращения утечек
	pathResult.Clear()

	// Открываем очередь
	result, err := oleutil.CallMethod(queueInfo, "Open", int32(accessMode), int32(MQ_DENY_NONE))
	if err != nil {
		return nil, fmt.Errorf("failed to open queue %s: %w", queuePath, err)
	}

	queue := result.ToIDispatch()
	if queue == nil {
		return nil, fmt.Errorf("failed to get queue dispatch")
	}

	// ✅ CRITICAL FIX: AddRef() + Clear() паттерн для освобождения Variant!
	// При высокой нагрузке (1000 msg/sec) без этого утечка = 100KB/sec
	queue.AddRef()
	result.Clear()

	return queue, nil
}

// createMessage создаёт объект MSMQMessage
func (m *MSMQ) createMessage() (*ole.IDispatch, func(), error) {
	msgUnknown, err := oleutil.CreateObject("MSMQ.MSMQMessage")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create MSMQ.MSMQMessage: %w", err)
	}

	msgDispatch, err := msgUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		msgUnknown.Release()
		return nil, nil, fmt.Errorf("failed to query IDispatch: %w", err)
	}

	cleanup := func() {
		msgDispatch.Release()
		msgUnknown.Release()
	}

	return msgDispatch, cleanup, nil
}

// setMessageBody устанавливает тело сообщения
func (m *MSMQ) setMessageBody(msgDispatch *ole.IDispatch, body []byte) error {
	result, err := oleutil.PutProperty(msgDispatch, "Body", string(body))
	if err != nil {
		return fmt.Errorf("failed to set Body: %w", err)
	}
	// ✅ CRITICAL FIX: Free Variant from PutProperty
	// Вызывается для каждого сообщения - критично!
	result.Clear()
	return nil
}

// sendMessage отправляет сообщение в очередь
func (m *MSMQ) sendMessage(msg *ole.IDispatch, queue *ole.IDispatch) error {
	result, err := oleutil.CallMethod(msg, "Send", queue)
	if err != nil {
		return err
	}
	// ✅ CRITICAL FIX: Free Variant from CallMethod
	// Вызывается для каждого отправленного сообщения
	result.Clear()
	return nil
}

// getMessageBody получает тело сообщения
func (m *MSMQ) getMessageBody(msgDispatch *ole.IDispatch) ([]byte, error) {
	bodyVariant, err := oleutil.GetProperty(msgDispatch, "Body")
	if err != nil {
		return nil, fmt.Errorf("failed to get Body: %w", err)
	}
	// ✅ CRITICAL FIX: освобождаем Variant после использования!
	// Для больших сообщений (XML пакеты) это критично
	defer bodyVariant.Clear()

	body := bodyVariant.Value()

	switch v := body.(type) {
	case []byte:
		return cleanXMLWrapper(v), nil
	case string:
		return cleanXMLWrapper([]byte(v)), nil
	default:
		return nil, fmt.Errorf("unknown body type: %T", body)
	}
}

// cleanXMLWrapper убирает XML обертку от MSMQ COM API
// MSMQ иногда оборачивает контент в <?xml version="1.0"?><string>...</string>
func cleanXMLWrapper(content []byte) []byte {
	s := string(content)

	// Проверяем наличие XML-обертки
	if strings.Contains(s, "<?xml version=\"1.0\"?>") && strings.Contains(s, "<string>") {
		startTag := "<string>"
		endTag := "</string>"

		startIdx := strings.Index(s, startTag)
		endIdx := strings.Index(s, endTag)

		if startIdx >= 0 && endIdx >= 0 && endIdx > startIdx {
			contentStart := startIdx + len(startTag)
			s = s[contentStart:endIdx]
		}
	}

	return []byte(s)
}

// normalizeQueuePath нормализует путь очереди
func (m *MSMQ) normalizeQueuePath(queueName string) string {
	// Если уже FormatName или содержит слеши, возвращаем как есть
	if strings.HasPrefix(queueName, "FormatName:") || strings.Contains(queueName, "\\") {
		return queueName
	}

	// Локальная очередь: используем простой формат .\private$\...
	hostname, _ := os.Hostname()
	if hostname == "" || hostname == "." {
		return fmt.Sprintf(".\\private$\\%s", queueName)
	}

	// Сетевая очередь: используем DIRECT format
	return fmt.Sprintf("FormatName:DIRECT=OS:%s\\private$\\%s", hostname, queueName)
}

// ============================================================================
// Вспомогательные функции
// ============================================================================

// isQueueEmptyError проверяет, является ли ошибка признаком пустой очереди
func isQueueEmptyError(err error) bool {
	if err == nil {
		return false
	}

	// Проверяем через ole.OleError
	if oleErr, ok := err.(*ole.OleError); ok {
		code := uint32(oleErr.Code())
		return code == MQ_ERROR_QUEUE_EMPTY || code == MQ_ERROR_IO_TIMEOUT || code == E_FAIL
	}

	// Fallback на строковую проверку
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "Queue is empty") ||
		strings.Contains(errStr, "0x80004005")
}
