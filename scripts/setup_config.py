#!/usr/bin/env python3
"""
TDTP Framework - Configuration Setup Script
============================================

Creates all necessary configuration files in correct locations.

Usage:
    python scripts/setup_config.py [--force]

Options:
    --force    Overwrite existing config files
"""

import os
import sys
import argparse
from pathlib import Path
from typing import Dict, Any


class ConfigSetup:
    def __init__(self, project_root: Path, force: bool = False):
        self.project_root = project_root
        self.force = force
        self.created_count = 0

    def write_file(self, path: Path, content: str, description: str):
        """Write file if it doesn't exist or force=True"""
        if path.exists() and not self.force:
            print(f"⊘ Skipping {path.relative_to(self.project_root)} (already exists, use --force to overwrite)")
            return False

        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        print(f"✓ Created {path.relative_to(self.project_root)} - {description}")
        self.created_count += 1
        return True

    def setup_wails_config(self):
        """Ensure wails.json is correct"""
        print("\n=== Setting up Wails Configuration ===")

        wails_config = self.project_root / 'cmd' / 'tdtp-xray' / 'wails.json'
        content = '''{
  "name": "tdtp-xray",
  "outputfilename": "tdtp-xray",
  "frontend:install": "",
  "frontend:build": "",
  "frontend:dev:watcher": "",
  "frontend:dev:serverUrl": "",
  "wailsjsdir": "./frontend/src",
  "assetdir": "./frontend/src",
  "reloaddirs": "./frontend/src",
  "debounceMS": 100
}
'''
        self.write_file(wails_config, content, "Wails application config")

    def setup_docker_compose(self):
        """Ensure main docker-compose.yml exists"""
        print("\n=== Setting up Docker Compose ===")

        docker_compose = self.project_root / 'docker-compose.yml'
        content = '''version: '3.8'

services:
  # ============================================
  # TDTP UNIFIED TEST ENVIRONMENT
  # ============================================

  # RabbitMQ - Message Broker
  rabbitmq:
    image: rabbitmq:3-management
    container_name: tdtp-rabbitmq-test
    ports:
      - "5672:5672"   # AMQP
      - "15672:15672" # Management UI http://localhost:15672
    environment:
      RABBITMQ_DEFAULT_USER: tdtp_test
      RABBITMQ_DEFAULT_PASS: tdtp_test_password
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - tdtp-network
    restart: unless-stopped

  # Microsoft SQL Server 2019
  mssql:
    image: mcr.microsoft.com/mssql/server:2019-latest
    container_name: tdtp-mssql-test
    ports:
      - "1433:1433"
    environment:
      ACCEPT_EULA: "Y"
      SA_PASSWORD: "YourStrong!Passw0rd"
      MSSQL_PID: "Developer"
    healthcheck:
      test: ["CMD-SHELL", "/opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -P 'YourStrong!Passw0rd' -Q 'SELECT 1' -C || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 10
    volumes:
      - mssql-data:/var/opt/mssql
    networks:
      - tdtp-network
    restart: unless-stopped

  # PostgreSQL 16
  postgres:
    image: postgres:16
    container_name: tdtp-postgres-test
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: tdtp_test
      POSTGRES_PASSWORD: tdtp_test_password
      POSTGRES_DB: tdtp_test_db
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tdtp_test"]
      interval: 10s
      timeout: 5s
      retries: 5
    volumes:
      - postgres-data:/var/lib/postgresql/data
    networks:
      - tdtp-network
    restart: unless-stopped

  # MySQL 8
  mysql:
    image: mysql:8
    container_name: tdtp-mysql-test
    ports:
      - "3306:3306"
    environment:
      MYSQL_ROOT_PASSWORD: "YourStrong!Passw0rd"
      MYSQL_DATABASE: tdtp_test_db
      MYSQL_USER: tdtp_test
      MYSQL_PASSWORD: tdtp_test_password
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-u", "root", "-pYourStrong!Passw0rd"]
      interval: 10s
      timeout: 5s
      retries: 5
    volumes:
      - mysql-data:/var/lib/mysql
    networks:
      - tdtp-network
    restart: unless-stopped

  # Adminer - Database Management UI
  adminer:
    image: adminer:latest
    container_name: tdtp-adminer
    ports:
      - "8080:8080"
    environment:
      ADMINER_DEFAULT_SERVER: tdtp-postgres-test
    networks:
      - tdtp-network
    restart: unless-stopped
    depends_on:
      - postgres
      - mssql
      - mysql

networks:
  tdtp-network:
    driver: bridge

volumes:
  mssql-data:
    name: tdtp-mssql-data
  postgres-data:
    name: tdtp-postgres-data
  mysql-data:
    name: tdtp-mysql-data
'''
        self.write_file(docker_compose, content, "Docker Compose unified environment")

    def setup_gitignore_additions(self):
        """Add important gitignore patterns"""
        print("\n=== Updating .gitignore ===")

        gitignore = self.project_root / '.gitignore'

        additional_patterns = '''
# TDTP X-Ray specific
cmd/tdtp-xray/frontend/dist/
cmd/tdtp-xray/build/
cmd/tdtp-xray/*.db
cmd/tdtp-xray/*.sqlite

# Development databases
*.db
*.sqlite

# Logs
*.log

# OS specific
.DS_Store
Thumbs.db

# IDE
.vscode/
.idea/
*.swp
*.swo

# Python
__pycache__/
*.pyc
*.pyo
*.egg-info/
.pytest_cache/

# Docker
docker-compose.override.yml
'''

        if gitignore.exists():
            content = gitignore.read_text()
            if '# TDTP X-Ray specific' not in content:
                with gitignore.open('a') as f:
                    f.write(additional_patterns)
                print(f"✓ Updated {gitignore.relative_to(self.project_root)} with additional patterns")
                self.created_count += 1
            else:
                print(f"⊘ .gitignore already contains TDTP patterns")
        else:
            self.write_file(gitignore, additional_patterns, "Git ignore patterns")

    def setup_env_template(self):
        """Create .env.template for environment variables"""
        print("\n=== Setting up Environment Template ===")

        env_template = self.project_root / '.env.template'
        content = '''# TDTP Framework Environment Variables Template
# Copy this file to .env and fill in your values

# ===== Database Credentials =====

# Microsoft SQL Server
MSSQL_HOST=localhost
MSSQL_PORT=1433
MSSQL_USER=sa
MSSQL_PASSWORD=YourStrong!Passw0rd
MSSQL_DATABASE=TravelGuide

# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=tdtp_test
POSTGRES_PASSWORD=tdtp_test_password
POSTGRES_DATABASE=TravelGuide

# MySQL
MYSQL_HOST=localhost
MYSQL_PORT=3306
MYSQL_USER=tdtp_test
MYSQL_PASSWORD=tdtp_test_password
MYSQL_DATABASE=tdtp_test_db

# RabbitMQ
RABBITMQ_HOST=localhost
RABBITMQ_PORT=5672
RABBITMQ_USER=tdtp_test
RABBITMQ_PASSWORD=tdtp_test_password

# ===== Application Settings =====

# Log level: debug, info, warn, error
LOG_LEVEL=info

# Development mode
DEV_MODE=true
'''
        self.write_file(env_template, content, "Environment variables template")

    def run(self):
        """Run all setup tasks"""
        print(f"\n{'='*60}")
        print(f"TDTP Framework Configuration Setup")
        print(f"Project: {self.project_root}")
        print(f"Force overwrite: {self.force}")
        print(f"{'='*60}")

        self.setup_wails_config()
        self.setup_docker_compose()
        self.setup_gitignore_additions()
        self.setup_env_template()

        # Summary
        print(f"\n{'='*60}")
        print(f"Summary:")
        print(f"  Configurations created/updated: {self.created_count}")
        print(f"{'='*60}\n")


def main():
    parser = argparse.ArgumentParser(description='Setup TDTP Framework configurations')
    parser.add_argument('--force', action='store_true', help='Overwrite existing files')
    args = parser.parse_args()

    # Find project root
    script_dir = Path(__file__).parent
    project_root = script_dir.parent

    setup = ConfigSetup(project_root, force=args.force)
    setup.run()

    print("✓ Configuration setup complete!")
    print("\nNext steps:")
    print("  1. Review configurations in project root")
    print("  2. Copy .env.template to .env and customize if needed")
    print("  3. Run: python scripts/devenv.py")


if __name__ == '__main__':
    main()
