#!/usr/bin/env python3
"""
TDTP Framework - Development Environment Setup Script
======================================================

Starts Docker containers and loads test databases.

Usage:
    python scripts/devenv.py [command]

Commands:
    start    - Start Docker environment (default)
    stop     - Stop Docker environment
    restart  - Restart Docker environment
    status   - Show status of all containers
    logs     - Show logs from all containers
    db-setup - Setup test databases (TravelGuide)
    db-check - Check database connections
"""

import os
import sys
import time
import subprocess
import argparse
from pathlib import Path
from typing import List, Optional


class DevEnvironment:
    def __init__(self, project_root: Path):
        self.project_root = project_root
        self.docker_compose = project_root / 'docker-compose.yml'

        # Database credentials (from docker-compose.yml)
        self.mssql_password = 'YourStrong!Passw0rd'
        self.postgres_user = 'tdtp_test'
        self.postgres_password = 'tdtp_test_password'
        self.mysql_user = 'tdtp_test'
        self.mysql_password = 'tdtp_test_password'

    def run_command(self, cmd: List[str], check: bool = True, capture: bool = False) -> Optional[subprocess.CompletedProcess]:
        """Run shell command"""
        try:
            if capture:
                result = subprocess.run(cmd, check=check, capture_output=True, text=True)
                return result
            else:
                subprocess.run(cmd, check=check)
                return None
        except subprocess.CalledProcessError as e:
            print(f"✗ Command failed: {' '.join(cmd)}")
            print(f"  Error: {e}")
            return None
        except FileNotFoundError:
            print(f"✗ Command not found: {cmd[0]}")
            print(f"  Make sure Docker/docker-compose is installed and in PATH")
            return None

    def docker_compose_cmd(self, *args) -> List[str]:
        """Build docker-compose command"""
        return ['docker-compose', '-f', str(self.docker_compose)] + list(args)

    def start(self):
        """Start Docker environment"""
        print("\n=== Starting TDTP Development Environment ===\n")

        if not self.docker_compose.exists():
            print(f"✗ docker-compose.yml not found at {self.docker_compose}")
            print(f"  Run: python scripts/setup_config.py first")
            return False

        print("Starting Docker containers...")
        cmd = self.docker_compose_cmd('up', '-d')
        if self.run_command(cmd) is None:
            return False

        print("\nWaiting for containers to be healthy...")
        time.sleep(5)

        # Check status
        self.status()

        print("\n✓ Environment started successfully!")
        print("\nServices available at:")
        print("  • RabbitMQ Management: http://localhost:15672 (tdtp_test/tdtp_test_password)")
        print("  • Adminer:            http://localhost:8080")
        print("  • MSSQL:              localhost:1433 (sa/YourStrong!Passw0rd)")
        print("  • PostgreSQL:         localhost:5432 (tdtp_test/tdtp_test_password)")
        print("  • MySQL:              localhost:3306 (tdtp_test/tdtp_test_password)")

        return True

    def stop(self):
        """Stop Docker environment"""
        print("\n=== Stopping TDTP Development Environment ===\n")

        cmd = self.docker_compose_cmd('stop')
        self.run_command(cmd)

        print("\n✓ Environment stopped")

    def restart(self):
        """Restart Docker environment"""
        print("\n=== Restarting TDTP Development Environment ===\n")

        cmd = self.docker_compose_cmd('restart')
        self.run_command(cmd)

        print("\n✓ Environment restarted")

    def status(self):
        """Show status of containers"""
        print("\n=== Container Status ===\n")

        cmd = self.docker_compose_cmd('ps')
        self.run_command(cmd, check=False)

    def logs(self, follow: bool = False):
        """Show logs from containers"""
        print("\n=== Container Logs ===\n")

        cmd = self.docker_compose_cmd('logs')
        if follow:
            cmd.append('-f')
        self.run_command(cmd, check=False)

    def setup_mssql_database(self):
        """Setup MSSQL TravelGuide database"""
        print("\n--- Setting up MSSQL TravelGuide Database ---")

        # Check if container is running
        result = self.run_command(['docker', 'ps', '--filter', 'name=tdtp-mssql-test', '--format', '{{.Names}}'],
                                   capture=True)
        if not result or 'tdtp-mssql-test' not in result.stdout:
            print("✗ MSSQL container not running. Start environment first.")
            return False

        # Wait for MSSQL to be ready
        print("Waiting for MSSQL to be ready...")
        max_attempts = 60  # 2 minutes
        for i in range(max_attempts):
            result = self.run_command(
                ['docker', 'exec', 'tdtp-mssql-test', '/opt/mssql-tools18/bin/sqlcmd',
                 '-S', 'localhost', '-U', 'sa', '-P', self.mssql_password, '-Q', 'SELECT 1', '-C'],
                check=False, capture=True
            )
            if result and result.returncode == 0:
                print("✓ MSSQL is ready")
                break

            if (i + 1) % 10 == 0:
                print(f"  Still waiting... ({i + 1}/{max_attempts})")

            time.sleep(2)
        else:
            print("✗ MSSQL failed to become ready after 2 minutes")
            print("  Check logs: docker logs tdtp-mssql-test")
            return False

        # Run setup script
        setup_sql = self.project_root / 'examples' / 'travel-guide' / 'setup_database.sql'
        if not setup_sql.exists():
            print(f"✗ Setup script not found: {setup_sql}")
            return False

        print(f"Running {setup_sql.name}...")

        # Execute SQL via stdin
        with open(setup_sql, 'r') as f:
            proc = subprocess.Popen(
                ['docker', 'exec', '-i', 'tdtp-mssql-test', '/opt/mssql-tools18/bin/sqlcmd',
                 '-S', 'localhost', '-U', 'sa', '-P', self.mssql_password, '-C'],
                stdin=f,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True
            )
            stdout, stderr = proc.communicate()

            if proc.returncode != 0:
                print(f"✗ SQL script failed: {stderr}")
                return False

            print("✓ SQL script executed successfully")

        # Load data with Python script
        populate_script = self.project_root / 'examples' / 'travel-guide' / 'populate_data.py'
        if populate_script.exists():
            print(f"Loading data with {populate_script.name}...")
            self.run_command(['python3', str(populate_script)], check=False)

        print("✓ MSSQL TravelGuide database setup complete")
        return True

    def setup_postgres_database(self):
        """Setup PostgreSQL TravelGuide database"""
        print("\n--- Setting up PostgreSQL TravelGuide Database ---")

        # Check if container is running
        result = self.run_command(['docker', 'ps', '--filter', 'name=tdtp-postgres-test', '--format', '{{.Names}}'],
                                   capture=True)
        if not result or 'tdtp-postgres-test' not in result.stdout:
            print("✗ PostgreSQL container not running. Start environment first.")
            return False

        # Create database
        print("Creating TravelGuide database...")
        self.run_command(
            ['docker', 'exec', 'tdtp-postgres-test', 'psql', '-U', self.postgres_user,
             '-d', 'tdtp_test_db', '-c', 'CREATE DATABASE "TravelGuide";'],
            check=False
        )

        # Run setup script
        setup_sql = self.project_root / 'examples' / 'travel-guide' / 'setup_database_postgres.sql'
        if not setup_sql.exists():
            print(f"✗ Setup script not found: {setup_sql}")
            return False

        print(f"Running {setup_sql.name}...")

        # Execute SQL via stdin
        with open(setup_sql, 'r') as f:
            proc = subprocess.Popen(
                ['docker', 'exec', '-i', 'tdtp-postgres-test', 'psql', '-U', self.postgres_user, '-d', 'TravelGuide'],
                stdin=f,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True
            )
            stdout, stderr = proc.communicate()

            if proc.returncode != 0:
                print(f"✗ SQL script failed: {stderr}")
                return False

            print("✓ SQL script executed successfully")

        # Load data with Python script
        populate_script = self.project_root / 'examples' / 'travel-guide' / 'populate_data_postgres.py'
        if populate_script.exists():
            print(f"Loading data with {populate_script.name}...")
            self.run_command(['python3', str(populate_script)], check=False)

        print("✓ PostgreSQL TravelGuide database setup complete")
        return True

    def setup_mssql_travelagency(self):
        """Setup MSSQL TravelAgency database"""
        print("\n--- Setting up MSSQL TravelAgency Database ---")

        # Create database
        print("Creating TravelAgency database...")
        self.run_command(
            ['docker', 'exec', 'tdtp-mssql-test', '/opt/mssql-tools18/bin/sqlcmd',
             '-S', 'localhost', '-U', 'sa', '-P', self.mssql_password,
             '-Q', 'CREATE DATABASE TravelAgency', '-C'],
            check=False
        )

        # Run setup script
        setup_sql = self.project_root / 'examples' / 'travel-agency' / 'setup_database.sql'
        if not setup_sql.exists():
            print(f"✗ Setup script not found: {setup_sql}")
            return False

        print(f"Running {setup_sql.name}...")

        with open(setup_sql, 'r') as f:
            proc = subprocess.Popen(
                ['docker', 'exec', '-i', 'tdtp-mssql-test', '/opt/mssql-tools18/bin/sqlcmd',
                 '-S', 'localhost', '-U', 'sa', '-P', self.mssql_password, '-d', 'TravelAgency', '-C'],
                stdin=f,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True
            )
            stdout, stderr = proc.communicate()

            if proc.returncode != 0:
                print(f"✗ SQL script failed: {stderr}")
                return False

            print("✓ SQL script executed successfully")

        # Load data
        populate_script = self.project_root / 'examples' / 'travel-agency' / 'populate_data.py'
        if populate_script.exists():
            print(f"Loading data with {populate_script.name}...")
            self.run_command(['python3', str(populate_script)], check=False)

        print("✓ MSSQL TravelAgency database setup complete")
        return True

    def setup_postgres_travelagency(self):
        """Setup PostgreSQL TravelAgency database"""
        print("\n--- Setting up PostgreSQL TravelAgency Database ---")

        # Create database
        print("Creating TravelAgency database...")
        self.run_command(
            ['docker', 'exec', 'tdtp-postgres-test', 'psql', '-U', self.postgres_user,
             '-d', 'tdtp_test_db', '-c', 'CREATE DATABASE "TravelAgency";'],
            check=False
        )

        # Run setup script
        setup_sql = self.project_root / 'examples' / 'travel-agency' / 'setup_database_postgres.sql'
        if not setup_sql.exists():
            print(f"✗ Setup script not found: {setup_sql}")
            return False

        print(f"Running {setup_sql.name}...")

        with open(setup_sql, 'r') as f:
            proc = subprocess.Popen(
                ['docker', 'exec', '-i', 'tdtp-postgres-test', 'psql', '-U', self.postgres_user, '-d', 'TravelAgency'],
                stdin=f,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True
            )
            stdout, stderr = proc.communicate()

            if proc.returncode != 0:
                print(f"✗ SQL script failed: {stderr}")
                return False

            print("✓ SQL script executed successfully")

        # Load data
        populate_script = self.project_root / 'examples' / 'travel-agency' / 'populate_data_postgres.py'
        if populate_script.exists():
            print(f"Loading data with {populate_script.name}...")
            self.run_command(['python3', str(populate_script)], check=False)

        print("✓ PostgreSQL TravelAgency database setup complete")
        return True

    def setup_databases(self):
        """Setup all test databases"""
        print("\n=== Setting up Test Databases ===")

        self.setup_mssql_database()
        self.setup_postgres_database()
        self.setup_mssql_travelagency()
        self.setup_postgres_travelagency()

        print("\n✓ All databases setup complete!")

    def check_databases(self):
        """Check database connections"""
        print("\n=== Checking Database Connections ===\n")

        # MSSQL
        print("MSSQL (localhost:1433):", end=" ")
        result = self.run_command(
            ['docker', 'exec', 'tdtp-mssql-test', '/opt/mssql-tools18/bin/sqlcmd',
             '-S', 'localhost', '-U', 'sa', '-P', self.mssql_password,
             '-d', 'TravelGuide', '-Q', 'SELECT COUNT(*) FROM cities'],
            check=False, capture=True
        )
        if result and result.returncode == 0:
            print("✓ Connected")
        else:
            print("✗ Failed")

        # PostgreSQL
        print("PostgreSQL (localhost:5432):", end=" ")
        result = self.run_command(
            ['docker', 'exec', 'tdtp-postgres-test', 'psql', '-U', self.postgres_user,
             '-d', 'TravelGuide', '-c', 'SELECT COUNT(*) FROM cities;'],
            check=False, capture=True
        )
        if result and result.returncode == 0:
            print("✓ Connected")
        else:
            print("✗ Failed")

        # MySQL
        print("MySQL (localhost:3306):", end=" ")
        result = self.run_command(
            ['docker', 'exec', 'tdtp-mysql-test', 'mysql', '-u', self.mysql_user,
             f'-p{self.mysql_password}', '-e', 'SELECT 1'],
            check=False, capture=True
        )
        if result and result.returncode == 0:
            print("✓ Connected")
        else:
            print("✗ Failed")


def main():
    parser = argparse.ArgumentParser(description='TDTP Development Environment Manager')
    parser.add_argument('command', nargs='?', default='start',
                        choices=['start', 'stop', 'restart', 'status', 'logs', 'db-setup', 'db-check'],
                        help='Command to execute')
    parser.add_argument('-f', '--follow', action='store_true', help='Follow logs (for logs command)')
    args = parser.parse_args()

    # Find project root
    script_dir = Path(__file__).parent
    project_root = script_dir.parent

    env = DevEnvironment(project_root)

    # Execute command
    if args.command == 'start':
        if env.start():
            print("\nRun 'python scripts/devenv.py db-setup' to load test databases")
    elif args.command == 'stop':
        env.stop()
    elif args.command == 'restart':
        env.restart()
    elif args.command == 'status':
        env.status()
    elif args.command == 'logs':
        env.logs(follow=args.follow)
    elif args.command == 'db-setup':
        env.setup_databases()
    elif args.command == 'db-check':
        env.check_databases()


if __name__ == '__main__':
    main()
