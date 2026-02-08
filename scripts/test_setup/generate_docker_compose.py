#!/usr/bin/env python3
"""
Генерация docker-compose.yml для тестового окружения TDTP Framework
"""

import yaml
from pathlib import Path


def generate_docker_compose():
    """Создает docker-compose.yml с PostgreSQL, RabbitMQ и Kafka"""

    compose = {
        'version': '3.8',
        'services': {
            'postgres': {
                'image': 'postgres:15-alpine',
                'container_name': 'tdtp_postgres',
                'environment': {
                    'POSTGRES_USER': 'postgres',
                    'POSTGRES_PASSWORD': 'postgres',
                    'POSTGRES_DB': 'postgres'
                },
                'ports': ['5432:5432'],
                'volumes': ['postgres_data:/var/lib/postgresql/data'],
                'healthcheck': {
                    'test': ['CMD-SHELL', 'pg_isready -U postgres'],
                    'interval': '10s',
                    'timeout': '5s',
                    'retries': 5
                }
            },
            'rabbitmq': {
                'image': 'rabbitmq:3-management-alpine',
                'container_name': 'tdtp_rabbitmq',
                'environment': {
                    'RABBITMQ_DEFAULT_USER': 'guest',
                    'RABBITMQ_DEFAULT_PASS': 'guest'
                },
                'ports': [
                    '5672:5672',   # AMQP
                    '15672:15672'  # Management UI
                ],
                'volumes': ['rabbitmq_data:/var/lib/rabbitmq'],
                'healthcheck': {
                    'test': ['CMD', 'rabbitmq-diagnostics', 'ping'],
                    'interval': '30s',
                    'timeout': '10s',
                    'retries': 5
                }
            },
            'kafka': {
                'image': 'confluentinc/cp-kafka:7.5.0',
                'container_name': 'tdtp_kafka',
                'environment': {
                    'KAFKA_BROKER_ID': 1,
                    'KAFKA_LISTENER_SECURITY_PROTOCOL_MAP': 'PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT',
                    'KAFKA_ADVERTISED_LISTENERS': 'PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092',
                    'KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR': 1,
                    'KAFKA_TRANSACTION_STATE_LOG_MIN_ISR': 1,
                    'KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR': 1,
                    'KAFKA_ZOOKEEPER_CONNECT': 'zookeeper:2181'
                },
                'ports': ['9092:9092'],
                'depends_on': ['zookeeper']
            },
            'zookeeper': {
                'image': 'confluentinc/cp-zookeeper:7.5.0',
                'container_name': 'tdtp_zookeeper',
                'environment': {
                    'ZOOKEEPER_CLIENT_PORT': 2181,
                    'ZOOKEEPER_TICK_TIME': 2000
                },
                'ports': ['2181:2181']
            }
        },
        'volumes': {
            'postgres_data': {},
            'rabbitmq_data': {}
        }
    }

    # Сохранение в корень проекта
    output_path = Path(__file__).parent.parent.parent / 'docker-compose.yml'

    with open(output_path, 'w', encoding='utf-8') as f:
        yaml.dump(compose, f, default_flow_style=False, sort_keys=False)

    print(f"✓ docker-compose.yml создан: {output_path}")
    print("\nЗапуск:")
    print("  docker-compose up -d")
    print("\nПроверка:")
    print("  docker-compose ps")
    print("\nОстановка:")
    print("  docker-compose down")
    print("\nОстановка с удалением данных:")
    print("  docker-compose down -v")


if __name__ == '__main__':
    generate_docker_compose()
