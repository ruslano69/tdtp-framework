//go:build windows
// +build windows

package brokers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// MSMQ реализует MessageBroker для Microsoft Message Queuing (Windows only)
// Использует COM API через go-ole
type MSMQ struct {
	config      Config
	queueInfo   *ole.IDispatch
	sendQueue   *ole.IDispatch
	recvQueue   *ole.IDispatch
	initialized bool
}

// MSMQ константы доступа
const (
	MQ_SEND_ACCESS    = 2  // Для отправки сообщений
	MQ_RECEIVE_ACCESS = 1  // Для получения сообщений
	MQ_PEEK_ACCESS    = 32 // Для просмотра без удаления
	MQ_DENY_NONE      = 0  // Разделяемый доступ
	MQ_DENY_RECEIVE   = 1  // Эксклюзивное чтение
)

// NewMSMQ создает новый MSMQ брокер
func NewMSMQ(cfg Config) (*MSMQ, error) {
	// Валидация конфигурации
	if cfg.QueuePath == "" {
		return nil, fmt.Errorf("queue_path is required for MSMQ (example: \".\\private$\\tdtp_export\")")
	}

	// Нормализуем путь очереди
	queuePath := normalizeQueuePath(cfg.QueuePath)

	return &MSMQ{
		config: Config{
			Type:      "msmq",
			QueuePath: queuePath,
		},
		initialized: false,
	}, nil
}

// Connect устанавливает соединение с MSMQ через COM API
func (m *MSMQ) Connect(ctx context.Context) error {
	if m.initialized {
		return nil // Уже подключены
	}

	// Инициализируем COM
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err != nil {
		return fmt.Errorf("failed to initialize COM: %w", err)
	}

	// Создаем объект MSMQQueueInfo
	unknown, err := oleutil.CreateObject("MSMQ.MSMQQueueInfo")
	if err != nil {
		ole.CoUninitialize()
		return fmt.Errorf("failed to create MSMQ.MSMQQueueInfo object (is MSMQ installed?): %w", err)
	}

	m.queueInfo, err = unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		ole.CoUninitialize()
		return fmt.Errorf("failed to query IDispatch interface: %w", err)
	}

	// Устанавливаем путь к очереди (FormatName или PathName)
	_, err = oleutil.PutProperty(m.queueInfo, "PathName", m.config.QueuePath)
	if err != nil {
		m.queueInfo.Release()
		ole.CoUninitialize()
		return fmt.Errorf("failed to set queue path: %w", err)
	}

	// Проверяем существование очереди
	exists, err := m.queueExists()
	if err != nil {
		m.queueInfo.Release()
		ole.CoUninitialize()
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	// Если очередь не существует, создаем её
	if !exists {
		err = m.createQueue()
		if err != nil {
			m.queueInfo.Release()
			ole.CoUninitialize()
			return fmt.Errorf("failed to create queue: %w", err)
		}
	}

	m.initialized = true
	return nil
}

// Close закрывает соединение с MSMQ
func (m *MSMQ) Close() error {
	if !m.initialized {
		return nil
	}

	// Закрываем очереди
	if m.sendQueue != nil {
		_, err := oleutil.CallMethod(m.sendQueue, "Close")
		if err != nil {
			// Логируем, но не возвращаем ошибку
			fmt.Printf("Warning: failed to close send queue: %v\n", err)
		}
		m.sendQueue.Release()
		m.sendQueue = nil
	}

	if m.recvQueue != nil {
		_, err := oleutil.CallMethod(m.recvQueue, "Close")
		if err != nil {
			fmt.Printf("Warning: failed to close receive queue: %v\n", err)
		}
		m.recvQueue.Release()
		m.recvQueue = nil
	}

	// Освобождаем QueueInfo
	if m.queueInfo != nil {
		m.queueInfo.Release()
		m.queueInfo = nil
	}

	// Деинициализируем COM
	ole.CoUninitialize()
	m.initialized = false

	return nil
}

// Send отправляет сообщение в MSMQ очередь
func (m *MSMQ) Send(ctx context.Context, message []byte) error {
	if !m.initialized {
		return fmt.Errorf("not connected to MSMQ")
	}

	// Открываем очередь для отправки (если еще не открыта)
	if m.sendQueue == nil {
		result, err := oleutil.CallMethod(m.queueInfo, "Open", MQ_SEND_ACCESS, MQ_DENY_NONE)
		if err != nil {
			return fmt.Errorf("failed to open queue for sending: %w", err)
		}
		m.sendQueue = result.ToIDispatch()
	}

	// Создаем объект MSMQMessage
	msgUnknown, err := oleutil.CreateObject("MSMQ.MSMQMessage")
	if err != nil {
		return fmt.Errorf("failed to create MSMQ.MSMQMessage object: %w", err)
	}
	defer msgUnknown.Release()

	msgDispatch, err := msgUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("failed to query IDispatch interface for message: %w", err)
	}
	defer msgDispatch.Release()

	// Устанавливаем тело сообщения
	_, err = oleutil.PutProperty(msgDispatch, "Body", message)
	if err != nil {
		return fmt.Errorf("failed to set message body: %w", err)
	}

	// Устанавливаем метку сообщения
	_, err = oleutil.PutProperty(msgDispatch, "Label", "TDTP Packet")
	if err != nil {
		return fmt.Errorf("failed to set message label: %w", err)
	}

	// Отправляем сообщение
	_, err = oleutil.CallMethod(m.sendQueue, "Send", msgDispatch)
	if err != nil {
		return fmt.Errorf("failed to send message to MSMQ: %w", err)
	}

	return nil
}

// Receive получает сообщение из MSMQ очереди
func (m *MSMQ) Receive(ctx context.Context) ([]byte, error) {
	if !m.initialized {
		return nil, fmt.Errorf("not connected to MSMQ")
	}

	// Открываем очередь для получения (если еще не открыта)
	if m.recvQueue == nil {
		result, err := oleutil.CallMethod(m.queueInfo, "Open", MQ_RECEIVE_ACCESS, MQ_DENY_NONE)
		if err != nil {
			return nil, fmt.Errorf("failed to open queue for receiving: %w", err)
		}
		m.recvQueue = result.ToIDispatch()
	}

	// Получаем сообщение с таймаутом 1 секунда
	result, err := oleutil.CallMethod(m.recvQueue, "Receive", nil, nil, nil, 1000)
	if err != nil {
		// Проверяем, не timeout ли это
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "Queue is empty") {
			return nil, fmt.Errorf("no messages available in queue")
		}
		return nil, fmt.Errorf("failed to receive message: %w", err)
	}

	msgDispatch := result.ToIDispatch()
	if msgDispatch == nil {
		return nil, fmt.Errorf("no messages available in queue")
	}
	defer msgDispatch.Release()

	// Получаем тело сообщения
	bodyVariant, err := oleutil.GetProperty(msgDispatch, "Body")
	if err != nil {
		return nil, fmt.Errorf("failed to get message body: %w", err)
	}

	// Конвертируем VARIANT в []byte
	body, ok := bodyVariant.Value().([]byte)
	if !ok {
		// Пробуем получить как string
		bodyStr, ok := bodyVariant.Value().(string)
		if ok {
			body = []byte(bodyStr)
		} else {
			return nil, fmt.Errorf("failed to convert message body to bytes")
		}
	}

	return body, nil
}

// Ping проверяет доступность MSMQ и очереди
func (m *MSMQ) Ping(ctx context.Context) error {
	if !m.initialized {
		return fmt.Errorf("not connected to MSMQ")
	}

	// Проверяем существование очереди
	exists, err := m.queueExists()
	if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("queue does not exist: %s", m.config.QueuePath)
	}

	return nil
}

// GetBrokerType возвращает тип брокера
func (m *MSMQ) GetBrokerType() string {
	return "msmq"
}

// queueExists проверяет существование очереди через MSMQQuery
func (m *MSMQ) queueExists() (bool, error) {
	// Используем MSMQQuery для проверки
	queryUnknown, err := oleutil.CreateObject("MSMQ.MSMQQuery")
	if err != nil {
		return false, fmt.Errorf("failed to create MSMQ.MSMQQuery object: %w", err)
	}
	defer queryUnknown.Release()

	queryDispatch, err := queryUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return false, fmt.Errorf("failed to query IDispatch interface: %w", err)
	}
	defer queryDispatch.Release()

	// Ищем очередь по пути
	result, err := oleutil.CallMethod(queryDispatch, "LookupQueue", nil, nil, nil, m.config.QueuePath)
	if err != nil {
		// Если очередь не найдена, это не ошибка
		return false, nil
	}

	queueInfos := result.ToIDispatch()
	if queueInfos == nil {
		return false, nil
	}
	defer queueInfos.Release()

	// Проверяем, есть ли результаты
	nextResult, err := oleutil.CallMethod(queueInfos, "Next")
	if err != nil {
		return false, nil
	}

	nextQueue := nextResult.ToIDispatch()
	if nextQueue == nil {
		return false, nil
	}
	defer nextQueue.Release()

	return true, nil
}

// createQueue создает новую MSMQ очередь
func (m *MSMQ) createQueue() error {
	// Создаем очередь через Create()
	_, err := oleutil.CallMethod(m.queueInfo, "Create", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	return nil
}

// normalizeQueuePath нормализует путь очереди
func normalizeQueuePath(path string) string {
	// Убираем лишние пробелы
	path = strings.TrimSpace(path)

	// Если путь не начинается с ".\" или ".\private$", добавляем
	if !strings.HasPrefix(path, ".\\") && !strings.HasPrefix(path, "private$") {
		path = ".\\private$\\" + path
	}

	return path
}
