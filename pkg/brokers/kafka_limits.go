//go:build !nokafka

package brokers

// kafka_limits.go — размер сообщения глазами брокера, а не клиента.
//
// Kafka отвергает produce-запрос по message.max.bytes, и меряет он сжатый
// record batch целиком, а не отдельную запись. Клиентский BatchBytes к этому
// отношения не имеет: он лишь говорит, сколько kafka-go согласится упаковать,
// и выставленный в 100 МБ он всего лишь позволяет собрать запрос, который
// брокер потом отвергнет.
//
// Поэтому лимит спрашивается у самого брокера. Ошибка "Message Size Too Large"
// приходит уже после того, как данные прочитаны из БД и сериализованы —
// предупредить заранее дешевле, чем разбирать её постфактум.

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/segmentio/kafka-go"
)

// DefaultBrokerMessageMaxBytes — значение Kafka по умолчанию (1 MB + запас на
// заголовки записи). Используется, если брокер не ответил: лучше предупредить
// по консервативной оценке, чем молчать.
const DefaultBrokerMessageMaxBytes = 1048588

// RecommendedMessageMaxBytes — что стоит поставить брокеру под TDTP.
//
// Часть пакета по умолчанию — около 1.9 МБ несжатого XML; со сжатием обычно
// втрое меньше, но коэффициент зависит от данных и на высокоэнтропийных
// (UUID, base64, шифротекст) стремится к единице. 4 МБ покрывают часть
// целиком даже когда сжатие не помогает, и оставляют запас на батч из
// нескольких мелких частей.
const RecommendedMessageMaxBytes = 4 * 1024 * 1024

// messageMaxBytes возвращает предел, действующий для топика этого брокера.
//
// Спрашивается дважды: у топика и у брокера. Топик может переопределить
// max.message.bytes, и тогда именно его значение решает — брокерское
// становится неважным.
func (k *Kafka) messageMaxBytes(ctx context.Context) (int, error) {
	client := &kafka.Client{Addr: kafka.TCP(k.config.Brokers...)}

	// Топик первым: его настройка перекрывает брокерскую.
	if v, err := describeInt(ctx, client, kafka.ResourceTypeTopic, k.config.Topic, "max.message.bytes"); err == nil && v > 0 {
		return v, nil
	}

	// Брокер адресуется своим node id — пустое имя ресурса kafka-go не
	// принимает: оно уходит в strconv.Atoi и падает на "invalid syntax".
	// Поэтому id сначала выясняется через метаданные.
	id, err := anyBrokerID(ctx, client)
	if err != nil {
		return 0, err
	}

	v, err := describeInt(ctx, client, kafka.ResourceTypeBroker, id, "message.max.bytes")
	if err != nil {
		return 0, err
	}

	return v, nil
}

// anyBrokerID возвращает id любого узла кластера.
//
// Любого достаточно: message.max.bytes задаётся одинаково на всех узлах, а
// расхождение между ними — уже неисправный кластер, и предупреждать о размере
// в этом случае не самая насущная задача.
func anyBrokerID(ctx context.Context, client *kafka.Client) (string, error) {
	resp, err := client.Metadata(ctx, &kafka.MetadataRequest{})
	if err != nil {
		return "", fmt.Errorf("metadata: %w", err)
	}
	if len(resp.Brokers) == 0 {
		return "", fmt.Errorf("metadata: cluster reported no brokers")
	}

	return strconv.Itoa(resp.Brokers[0].ID), nil
}

// describeInt читает одно числовое значение конфигурации ресурса.
func describeInt(ctx context.Context, client *kafka.Client, rt kafka.ResourceType, name, key string) (int, error) {
	resp, err := client.DescribeConfigs(ctx, &kafka.DescribeConfigsRequest{
		Resources: []kafka.DescribeConfigRequestResource{{
			ResourceType: rt,
			ResourceName: name,
			ConfigNames:  []string{key},
		}},
	})
	if err != nil {
		return 0, fmt.Errorf("describe %s: %w", key, err)
	}
	if len(resp.Resources) == 0 {
		return 0, fmt.Errorf("describe %s: no resources in response", key)
	}
	if resErr := resp.Resources[0].Error; resErr != nil {
		return 0, fmt.Errorf("describe %s: %w", key, resErr)
	}

	for _, entry := range resp.Resources[0].ConfigEntries {
		if entry.ConfigName != key {
			continue
		}
		v, convErr := strconv.Atoi(entry.ConfigValue)
		if convErr != nil {
			return 0, fmt.Errorf("describe %s: value %q is not a number", key, entry.ConfigValue)
		}
		return v, nil
	}

	return 0, fmt.Errorf("describe %s: not present in response", key)
}

// limitOnce кеширует результат: предел спрашивается один раз на соединение,
// а не на каждое сообщение.
type limitCache struct {
	once  sync.Once
	value int
	known bool
}

// MessageMaxBytes возвращает предел брокера и признак того, что он получен
// на самом деле. При неудаче отдаётся значение Kafka по умолчанию с
// known=false — вызывающий решает, насколько громко об этом говорить.
func (k *Kafka) MessageMaxBytes(ctx context.Context) (limit int, known bool) {
	k.limits.once.Do(func() {
		v, err := k.messageMaxBytes(ctx)
		if err != nil {
			k.limits.value = DefaultBrokerMessageMaxBytes
			k.limits.known = false
			return
		}
		k.limits.value = v
		k.limits.known = true
	})

	return k.limits.value, k.limits.known
}
