#!/usr/bin/env python3
"""
Генерация конфигурационных файлов для тестирования
"""

import yaml
from pathlib import Path


class ConfigGenerator:
    def __init__(self, output_dir=None):
        if output_dir is None:
            self.output_dir = Path(__file__).parent.parent.parent
        else:
            self.output_dir = Path(output_dir)

    def base_config(self):
        """Базовая конфигурация (общая для всех)"""
        return {
            'export': {
                'compress': False,
                'compress_level': 3
            },
            'resilience': {
                'circuit_breaker': {
                    'enabled': True,
                    'threshold': 5,
                    'timeout': 60,
                    'max_concurrent': 100,
                    'success_threshold': 2
                },
                'retry': {
                    'enabled': True,
                    'max_attempts': 3,
                    'strategy': 'exponential',
                    'initial_wait_ms': 1000,
                    'max_wait_ms': 30000,
                    'jitter': True
                }
            },
            'audit': {
                'enabled': True,
                'level': 'standard',
                'max_size_mb': 100
            }
        }

    def generate_postgres_config(self, database='tdtp_test'):
        """PostgreSQL конфигурация"""
        config = self.base_config()
        config['database'] = {
            'type': 'postgres',
            'host': 'localhost',
            'port': 5432,
            'database': database,
            'user': 'tdtp_user',
            'password': 'tdtp_pass',
            'sslmode': 'disable'
        }
        config['audit']['file'] = f'audit_postgres_{database}.log'

        filename = f'config.postgres.{database}.yaml'
        self.save_config(config, filename)
        return filename

    def generate_rabbitmq_config(self, database='tdtp_test', queue='tdtp_test_queue'):
        """RabbitMQ конфигурация"""
        config = self.base_config()
        config['database'] = {
            'type': 'postgres',
            'host': 'localhost',
            'port': 5432,
            'database': database,
            'user': 'tdtp_user',
            'password': 'tdtp_pass',
            'sslmode': 'disable'
        }
        config['broker'] = {
            'type': 'rabbitmq',
            'host': 'localhost',
            'port': 5672,
            'user': 'guest',
            'password': 'guest',
            'queue': queue
        }
        config['export']['compress'] = True
        config['audit']['file'] = f'audit_rabbitmq_{database}.log'

        filename = f'config.rabbitmq.{database}.yaml'
        self.save_config(config, filename)
        return filename

    def generate_kafka_config(self, database='tdtp_test', topic='tdtp_test_topic'):
        """Kafka конфигурация"""
        config = self.base_config()
        config['database'] = {
            'type': 'postgres',
            'host': 'localhost',
            'port': 5432,
            'database': database,
            'user': 'tdtp_user',
            'password': 'tdtp_pass',
            'sslmode': 'disable'
        }
        config['broker'] = {
            'type': 'kafka',
            'bootstrap_servers': 'localhost:9092',
            'topic': topic
        }
        config['export']['compress'] = True
        config['audit']['file'] = f'audit_kafka_{database}.log'

        filename = f'config.kafka.{database}.yaml'
        self.save_config(config, filename)
        return filename

    def save_config(self, config, filename):
        """Сохранить конфиг в файл"""
        filepath = self.output_dir / filename

        with open(filepath, 'w', encoding='utf-8') as f:
            yaml.dump(config, f, default_flow_style=False, sort_keys=False, allow_unicode=True)

        print(f"✓ {filename}")

    def generate_all(self):
        """Генерация всех конфигов"""
        print("=" * 60)
        print("ГЕНЕРАЦИЯ КОНФИГУРАЦИОННЫХ ФАЙЛОВ")
        print("=" * 60)
        print(f"\nДиректория: {self.output_dir}\n")

        configs = []

        # PostgreSQL конфиги
        print("PostgreSQL:")
        configs.append(self.generate_postgres_config('tdtp_test'))
        configs.append(self.generate_postgres_config('tdtp_target'))

        # RabbitMQ конфиги
        print("\nRabbitMQ:")
        configs.append(self.generate_rabbitmq_config('tdtp_test', 'tdtp_test_queue'))
        configs.append(self.generate_rabbitmq_config('tdtp_target', 'tdtp_test_queue'))

        # Kafka конфиги
        print("\nKafka:")
        configs.append(self.generate_kafka_config('tdtp_test', 'tdtp_test_topic'))
        configs.append(self.generate_kafka_config('tdtp_target', 'tdtp_test_topic'))

        print("\n" + "=" * 60)
        print("ИСПОЛЬЗОВАНИЕ")
        print("=" * 60)

        print("\nPostgreSQL:")
        print("  # Экспорт из tdtp_test")
        print("  tdtpcli --config config.postgres.tdtp_test.yaml --export users")

        print("\n  # Импорт в tdtp_target")
        print("  tdtpcli --config config.postgres.tdtp_target.yaml --import users.xml")

        print("\nRabbitMQ:")
        print("  # Экспорт в очередь")
        print("  tdtpcli --config config.rabbitmq.tdtp_test.yaml --export-broker users")

        print("\n  # Импорт из очереди")
        print("  tdtpcli --config config.rabbitmq.tdtp_target.yaml --import-broker")

        print("\nKafka:")
        print("  # Экспорт в топик")
        print("  tdtpcli --config config.kafka.tdtp_test.yaml --export-broker users")

        print("\n  # Импорт из топика")
        print("  tdtpcli --config config.kafka.tdtp_target.yaml --import-broker")

        print(f"\n✓ Создано {len(configs)} конфигурационных файлов")


if __name__ == '__main__':
    generator = ConfigGenerator()
    generator.generate_all()
