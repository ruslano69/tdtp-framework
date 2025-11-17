#!/usr/bin/env python3
"""
TDTP Framework - Docker Compose Generator

Утилита для генерации docker-compose.yml файлов с выбранными компонентами:
- Базы данных (PostgreSQL, MySQL, MSSQL, SQLite)
- Брокеры сообщений (RabbitMQ, Kafka)
- Дополнительные инструменты (pgAdmin, Adminer, Kafka UI)

Использование:
    python docker-compose-generator.py --interactive
    python docker-compose-generator.py --postgres --mysql --rabbitmq
    python docker-compose-generator.py --all --output custom-compose.yml
"""

import argparse
import sys
import yaml
from typing import List, Dict, Any


class DockerComposeGenerator:
    """Генератор docker-compose.yml для TDTP Framework"""

    def __init__(self):
        self.version = "3.8"
        self.services = {}
        self.volumes = {}
        self.networks = {
            "tdtp-network": {
                "driver": "bridge"
            }
        }

    def add_postgres(self):
        """Добавить PostgreSQL"""
        self.services["postgres"] = {
            "image": "postgres:16-alpine",
            "container_name": "tdtp-postgres",
            "environment": {
                "POSTGRES_USER": "tdtp",
                "POSTGRES_PASSWORD": "tdtp_password",
                "POSTGRES_DB": "tdtp_db"
            },
            "ports": ["5432:5432"],
            "volumes": ["postgres-data:/var/lib/postgresql/data"],
            "networks": ["tdtp-network"],
            "healthcheck": {
                "test": ["CMD-SHELL", "pg_isready -U tdtp"],
                "interval": "10s",
                "timeout": "5s",
                "retries": 5
            }
        }
        self.volumes["postgres-data"] = None

    def add_mysql(self):
        """Добавить MySQL"""
        self.services["mysql"] = {
            "image": "mysql:8.0",
            "container_name": "tdtp-mysql",
            "environment": {
                "MYSQL_ROOT_PASSWORD": "root_password",
                "MYSQL_DATABASE": "tdtp_db",
                "MYSQL_USER": "tdtp",
                "MYSQL_PASSWORD": "tdtp_password"
            },
            "ports": ["3306:3306"],
            "volumes": ["mysql-data:/var/lib/mysql"],
            "networks": ["tdtp-network"],
            "healthcheck": {
                "test": ["CMD", "mysqladmin", "ping", "-h", "localhost"],
                "interval": "10s",
                "timeout": "5s",
                "retries": 5
            }
        }
        self.volumes["mysql-data"] = None

    def add_mssql(self):
        """Добавить Microsoft SQL Server"""
        self.services["mssql"] = {
            "image": "mcr.microsoft.com/mssql/server:2022-latest",
            "container_name": "tdtp-mssql",
            "environment": {
                "ACCEPT_EULA": "Y",
                "SA_PASSWORD": "TdtpPassword123!",
                "MSSQL_PID": "Developer"
            },
            "ports": ["1433:1433"],
            "volumes": ["mssql-data:/var/opt/mssql"],
            "networks": ["tdtp-network"],
            "healthcheck": {
                "test": [
                    "CMD-SHELL",
                    "/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P TdtpPassword123! -Q 'SELECT 1'"
                ],
                "interval": "10s",
                "timeout": "5s",
                "retries": 5
            }
        }
        self.volumes["mssql-data"] = None

    def add_rabbitmq(self):
        """Добавить RabbitMQ"""
        self.services["rabbitmq"] = {
            "image": "rabbitmq:3.12-management-alpine",
            "container_name": "tdtp-rabbitmq",
            "environment": {
                "RABBITMQ_DEFAULT_USER": "tdtp",
                "RABBITMQ_DEFAULT_PASS": "tdtp_password"
            },
            "ports": [
                "5672:5672",   # AMQP
                "15672:15672"  # Management UI
            ],
            "volumes": ["rabbitmq-data:/var/lib/rabbitmq"],
            "networks": ["tdtp-network"],
            "healthcheck": {
                "test": ["CMD", "rabbitmq-diagnostics", "-q", "ping"],
                "interval": "10s",
                "timeout": "5s",
                "retries": 5
            }
        }
        self.volumes["rabbitmq-data"] = None

    def add_kafka(self):
        """Добавить Apache Kafka с Zookeeper"""
        self.services["zookeeper"] = {
            "image": "confluentinc/cp-zookeeper:7.5.0",
            "container_name": "tdtp-zookeeper",
            "environment": {
                "ZOOKEEPER_CLIENT_PORT": 2181,
                "ZOOKEEPER_TICK_TIME": 2000
            },
            "networks": ["tdtp-network"]
        }

        self.services["kafka"] = {
            "image": "confluentinc/cp-kafka:7.5.0",
            "container_name": "tdtp-kafka",
            "depends_on": ["zookeeper"],
            "environment": {
                "KAFKA_BROKER_ID": 1,
                "KAFKA_ZOOKEEPER_CONNECT": "zookeeper:2181",
                "KAFKA_ADVERTISED_LISTENERS": "PLAINTEXT://localhost:9092",
                "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR": 1,
                "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR": 1,
                "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": 1
            },
            "ports": ["9092:9092"],
            "networks": ["tdtp-network"],
            "healthcheck": {
                "test": ["CMD", "kafka-broker-api-versions", "--bootstrap-server", "localhost:9092"],
                "interval": "10s",
                "timeout": "10s",
                "retries": 5
            }
        }

    def add_pgadmin(self):
        """Добавить pgAdmin (UI для PostgreSQL)"""
        if "postgres" not in self.services:
            print("⚠️  Предупреждение: pgAdmin требует PostgreSQL. Добавляем PostgreSQL...")
            self.add_postgres()

        self.services["pgadmin"] = {
            "image": "dpage/pgadmin4:latest",
            "container_name": "tdtp-pgadmin",
            "environment": {
                "PGADMIN_DEFAULT_EMAIL": "admin@tdtp.local",
                "PGADMIN_DEFAULT_PASSWORD": "admin",
                "PGADMIN_CONFIG_SERVER_MODE": "False"
            },
            "ports": ["5050:80"],
            "networks": ["tdtp-network"],
            "depends_on": ["postgres"]
        }

    def add_adminer(self):
        """Добавить Adminer (универсальный UI для БД)"""
        self.services["adminer"] = {
            "image": "adminer:latest",
            "container_name": "tdtp-adminer",
            "ports": ["8080:8080"],
            "networks": ["tdtp-network"],
            "environment": {
                "ADMINER_DEFAULT_SERVER": "postgres"
            }
        }

    def add_kafka_ui(self):
        """Добавить Kafka UI"""
        if "kafka" not in self.services:
            print("⚠️  Предупреждение: Kafka UI требует Kafka. Добавляем Kafka...")
            self.add_kafka()

        self.services["kafka-ui"] = {
            "image": "provectuslabs/kafka-ui:latest",
            "container_name": "tdtp-kafka-ui",
            "depends_on": ["kafka"],
            "ports": ["8081:8080"],
            "networks": ["tdtp-network"],
            "environment": {
                "KAFKA_CLUSTERS_0_NAME": "tdtp-kafka",
                "KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS": "kafka:9092",
                "KAFKA_CLUSTERS_0_ZOOKEEPER": "zookeeper:2181"
            }
        }

    def generate_compose(self) -> Dict[str, Any]:
        """Генерировать итоговый docker-compose.yml"""
        compose = {
            "version": self.version,
            "services": self.services,
            "networks": self.networks
        }

        if self.volumes:
            compose["volumes"] = self.volumes

        return compose

    def save_to_file(self, filename: str = "docker-compose.yml"):
        """Сохранить в файл"""
        compose = self.generate_compose()

        with open(filename, 'w') as f:
            yaml.dump(compose, f, default_flow_style=False, sort_keys=False)

        print(f"✅ Файл {filename} успешно создан!")
        return filename


def interactive_mode():
    """Интерактивный режим выбора компонентов"""
    print("=" * 60)
    print("  TDTP Framework - Генератор Docker Compose")
    print("=" * 60)
    print()

    generator = DockerComposeGenerator()

    # Выбор баз данных
    print("📊 Базы данных:")
    if input("  Добавить PostgreSQL? (Y/n): ").lower() != 'n':
        generator.add_postgres()
        print("    ✓ PostgreSQL добавлен")

    if input("  Добавить MySQL? (Y/n): ").lower() != 'n':
        generator.add_mysql()
        print("    ✓ MySQL добавлен")

    if input("  Добавить MSSQL? (y/N): ").lower() == 'y':
        generator.add_mssql()
        print("    ✓ MSSQL добавлен")

    print()

    # Выбор брокеров
    print("📨 Брокеры сообщений:")
    if input("  Добавить RabbitMQ? (Y/n): ").lower() != 'n':
        generator.add_rabbitmq()
        print("    ✓ RabbitMQ добавлен")

    if input("  Добавить Kafka? (y/N): ").lower() == 'y':
        generator.add_kafka()
        print("    ✓ Kafka добавлен")

    print()

    # Выбор UI инструментов
    print("🖥️  UI инструменты:")
    if "postgres" in generator.services:
        if input("  Добавить pgAdmin? (y/N): ").lower() == 'y':
            generator.add_pgadmin()
            print("    ✓ pgAdmin добавлен")

    if input("  Добавить Adminer (универсальный UI)? (y/N): ").lower() == 'y':
        generator.add_adminer()
        print("    ✓ Adminer добавлен")

    if "kafka" in generator.services:
        if input("  Добавить Kafka UI? (y/N): ").lower() == 'y':
            generator.add_kafka_ui()
            print("    ✓ Kafka UI добавлен")

    print()
    print("-" * 60)

    # Сохранение
    filename = input("📝 Имя файла (docker-compose.yml): ").strip()
    if not filename:
        filename = "docker-compose.yml"

    generator.save_to_file(filename)
    print_summary(generator)
    print_usage_instructions()


def print_summary(generator: DockerComposeGenerator):
    """Вывести сводку по созданной конфигурации"""
    print()
    print("=" * 60)
    print("  Сводка конфигурации")
    print("=" * 60)

    print(f"\n📦 Сервисы ({len(generator.services)}):")
    for service_name in generator.services.keys():
        service = generator.services[service_name]
        ports = service.get("ports", [])
        if ports:
            print(f"  ✓ {service_name:15} → {', '.join(ports)}")
        else:
            print(f"  ✓ {service_name}")

    if generator.volumes:
        print(f"\n💾 Volumes ({len(generator.volumes)}):")
        for volume_name in generator.volumes.keys():
            print(f"  ✓ {volume_name}")


def print_usage_instructions():
    """Вывести инструкции по использованию"""
    print()
    print("=" * 60)
    print("  Инструкции по запуску")
    print("=" * 60)
    print()
    print("1️⃣  Запустить все сервисы:")
    print("    docker-compose up -d")
    print()
    print("2️⃣  Проверить статус:")
    print("    docker-compose ps")
    print()
    print("3️⃣  Посмотреть логи:")
    print("    docker-compose logs -f [service_name]")
    print()
    print("4️⃣  Остановить все сервисы:")
    print("    docker-compose down")
    print()
    print("5️⃣  Удалить все данные:")
    print("    docker-compose down -v")
    print()
    print("=" * 60)


def main():
    """Главная функция"""
    parser = argparse.ArgumentParser(
        description="Генератор docker-compose.yml для TDTP Framework",
        formatter_class=argparse.RawDescriptionHelpFormatter
    )

    parser.add_argument(
        "-i", "--interactive",
        action="store_true",
        help="Интерактивный режим"
    )

    parser.add_argument(
        "-o", "--output",
        default="docker-compose.yml",
        help="Имя выходного файла (по умолчанию: docker-compose.yml)"
    )

    # Базы данных
    parser.add_argument("--postgres", action="store_true", help="Добавить PostgreSQL")
    parser.add_argument("--mysql", action="store_true", help="Добавить MySQL")
    parser.add_argument("--mssql", action="store_true", help="Добавить MSSQL")

    # Брокеры
    parser.add_argument("--rabbitmq", action="store_true", help="Добавить RabbitMQ")
    parser.add_argument("--kafka", action="store_true", help="Добавить Kafka")

    # UI инструменты
    parser.add_argument("--pgadmin", action="store_true", help="Добавить pgAdmin")
    parser.add_argument("--adminer", action="store_true", help="Добавить Adminer")
    parser.add_argument("--kafka-ui", action="store_true", help="Добавить Kafka UI")

    # Все сразу
    parser.add_argument("--all", action="store_true", help="Добавить все компоненты")

    args = parser.parse_args()

    # Интерактивный режим
    if args.interactive or len(sys.argv) == 1:
        interactive_mode()
        return

    # Режим с аргументами
    generator = DockerComposeGenerator()

    if args.all:
        generator.add_postgres()
        generator.add_mysql()
        generator.add_rabbitmq()
        generator.add_kafka()
        generator.add_adminer()
        print("✅ Добавлены все компоненты")
    else:
        # Базы данных
        if args.postgres:
            generator.add_postgres()
            print("✅ PostgreSQL добавлен")
        if args.mysql:
            generator.add_mysql()
            print("✅ MySQL добавлен")
        if args.mssql:
            generator.add_mssql()
            print("✅ MSSQL добавлен")

        # Брокеры
        if args.rabbitmq:
            generator.add_rabbitmq()
            print("✅ RabbitMQ добавлен")
        if args.kafka:
            generator.add_kafka()
            print("✅ Kafka добавлен")

        # UI инструменты
        if args.pgadmin:
            generator.add_pgadmin()
            print("✅ pgAdmin добавлен")
        if args.adminer:
            generator.add_adminer()
            print("✅ Adminer добавлен")
        if args.kafka_ui:
            generator.add_kafka_ui()
            print("✅ Kafka UI добавлен")

    if not generator.services:
        print("❌ Ошибка: Не выбрано ни одного компонента!")
        print("Используйте --help для справки или --interactive для интерактивного режима")
        sys.exit(1)

    generator.save_to_file(args.output)
    print_summary(generator)
    print_usage_instructions()


if __name__ == "__main__":
    main()
