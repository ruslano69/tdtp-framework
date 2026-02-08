#!/usr/bin/env python3
"""
Главный скрипт для полной настройки тестового окружения
Запускает все шаги по порядку
"""

import subprocess
import sys
import time
from pathlib import Path


class TestSetup:
    def __init__(self):
        self.scripts_dir = Path(__file__).parent
        self.project_root = self.scripts_dir.parent.parent

    def run_command(self, cmd, check=True):
        """Запуск команды в shell"""
        print(f"\n→ Выполнение: {cmd}")
        result = subprocess.run(cmd, shell=True, cwd=self.project_root)
        if check and result.returncode != 0:
            print(f"✗ Команда завершилась с ошибкой: {result.returncode}")
            sys.exit(1)
        return result.returncode == 0

    def run_python_script(self, script_name):
        """Запуск Python скрипта"""
        script_path = self.scripts_dir / script_name
        print(f"\n{'=' * 80}")
        print(f"Запуск: {script_name}")
        print('=' * 80)
        result = subprocess.run([sys.executable, str(script_path)])
        if result.returncode != 0:
            print(f"✗ Скрипт завершился с ошибкой: {result.returncode}")
            return False
        return True

    def check_docker(self):
        """Проверка наличия Docker"""
        print("\n" + "=" * 80)
        print("ШАГ 1: ПРОВЕРКА DOCKER")
        print("=" * 80)

        if not self.run_command("docker --version", check=False):
            print("✗ Docker не установлен!")
            print("\nУстановите Docker:")
            print("  Windows: https://docs.docker.com/desktop/install/windows-install/")
            print("  Linux: https://docs.docker.com/engine/install/")
            return False

        if not self.run_command("docker-compose --version", check=False):
            print("✗ docker-compose не установлен!")
            return False

        print("✓ Docker и docker-compose установлены")
        return True

    def generate_docker_compose(self):
        """Генерация docker-compose.yml"""
        print("\n" + "=" * 80)
        print("ШАГ 2: ГЕНЕРАЦИЯ DOCKER-COMPOSE.YML")
        print("=" * 80)
        return self.run_python_script("generate_docker_compose.py")

    def start_docker_services(self):
        """Запуск Docker контейнеров"""
        print("\n" + "=" * 80)
        print("ШАГ 3: ЗАПУСК DOCKER КОНТЕЙНЕРОВ")
        print("=" * 80)

        print("\nОстановка существующих контейнеров...")
        self.run_command("docker-compose down", check=False)

        print("\nЗапуск новых контейнеров...")
        if not self.run_command("docker-compose up -d"):
            return False

        print("\nОжидание готовности сервисов (30 сек)...")
        time.sleep(30)

        print("\nПроверка статуса контейнеров:")
        self.run_command("docker-compose ps")

        return True

    def init_postgres(self):
        """Инициализация PostgreSQL"""
        print("\n" + "=" * 80)
        print("ШАГ 4: ИНИЦИАЛИЗАЦИЯ POSTGRESQL")
        print("=" * 80)
        return self.run_python_script("init_postgres.py")

    def generate_configs(self):
        """Генерация конфигурационных файлов"""
        print("\n" + "=" * 80)
        print("ШАГ 5: ГЕНЕРАЦИЯ КОНФИГУРАЦИОННЫХ ФАЙЛОВ")
        print("=" * 80)
        return self.run_python_script("generate_configs.py")

    def generate_test_data(self, count=10000):
        """Генерация тестовых данных"""
        print("\n" + "=" * 80)
        print("ШАГ 6: ГЕНЕРАЦИЯ ТЕСТОВЫХ ДАННЫХ")
        print("=" * 80)

        script_path = self.scripts_dir / "generate_test_data.py"
        result = subprocess.run([sys.executable, str(script_path), "--count", str(count)])

        return result.returncode == 0

    def show_summary(self):
        """Показать итоговую информацию"""
        print("\n" + "=" * 80)
        print("УСТАНОВКА ЗАВЕРШЕНА!")
        print("=" * 80)

        print("\n📦 Запущенные сервисы:")
        print("  - PostgreSQL: localhost:5432")
        print("  - RabbitMQ: localhost:5672 (Management UI: http://localhost:15672)")
        print("  - Kafka: localhost:9092")

        print("\n🗄️ Базы данных:")
        print("  - tdtp_test (источник)")
        print("  - tdtp_target (приемник)")

        print("\n📝 Конфигурационные файлы:")
        print("  - config.postgres.tdtp_test.yaml")
        print("  - config.postgres.tdtp_target.yaml")
        print("  - config.rabbitmq.tdtp_test.yaml")
        print("  - config.rabbitmq.tdtp_target.yaml")
        print("  - config.kafka.tdtp_test.yaml")
        print("  - config.kafka.tdtp_target.yaml")

        print("\n🧪 Готовые команды для тестирования:")

        print("\n1. Экспорт в файл:")
        print("   tdtpcli --config config.postgres.tdtp_test.yaml --export users --output users.xml")

        print("\n2. Импорт из файла:")
        print("   tdtpcli --config config.postgres.tdtp_target.yaml --import users.xml")

        print("\n3. Экспорт в RabbitMQ:")
        print("   tdtpcli --config config.rabbitmq.tdtp_test.yaml --export-broker users")

        print("\n4. Импорт из RabbitMQ:")
        print("   tdtpcli --config config.rabbitmq.tdtp_target.yaml --import-broker")

        print("\n📚 Дополнительно:")
        print("  - Логи Docker: docker-compose logs -f")
        print("  - Подключение к PostgreSQL: docker exec -it tdtp_postgres psql -U tdtp_user -d tdtp_test")
        print("  - RabbitMQ UI: http://localhost:15672 (guest/guest)")

        print("\n🛑 Остановка окружения:")
        print("  docker-compose down")
        print("  docker-compose down -v  # с удалением данных")

    def run_all(self, skip_docker=False, skip_data=False, data_count=10000):
        """Запуск всех шагов"""
        print("=" * 80)
        print("ПОЛНАЯ НАСТРОЙКА ТЕСТОВОГО ОКРУЖЕНИЯ TDTP FRAMEWORK")
        print("=" * 80)

        steps = [
            ("Проверка Docker", self.check_docker, not skip_docker),
            ("Генерация docker-compose.yml", self.generate_docker_compose, not skip_docker),
            ("Запуск Docker контейнеров", self.start_docker_services, not skip_docker),
            ("Инициализация PostgreSQL", self.init_postgres, True),
            ("Генерация конфигов", self.generate_configs, True),
            ("Генерация тестовых данных", lambda: self.generate_test_data(data_count), not skip_data),
        ]

        for step_name, step_func, should_run in steps:
            if not should_run:
                print(f"\n⏭️  Пропуск: {step_name}")
                continue

            if not step_func():
                print(f"\n✗ Ошибка на шаге: {step_name}")
                return False

        self.show_summary()
        return True


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description='Полная настройка тестового окружения TDTP Framework'
    )
    parser.add_argument('--skip-docker', action='store_true',
                        help='Пропустить настройку Docker (если уже запущен)')
    parser.add_argument('--skip-data', action='store_true',
                        help='Пропустить генерацию тестовых данных')
    parser.add_argument('--count', type=int, default=10000,
                        help='Количество тестовых записей (по умолчанию: 10000)')

    args = parser.parse_args()

    setup = TestSetup()
    success = setup.run_all(
        skip_docker=args.skip_docker,
        skip_data=args.skip_data,
        data_count=args.count
    )

    sys.exit(0 if success else 1)


if __name__ == '__main__':
    main()
