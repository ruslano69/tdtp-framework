#!/bin/bash
# ============================================
# Database Views Setup Helper Script
# ============================================
# This script helps apply view setup scripts to different databases
#
# Usage:
#   ./scripts/setup-views.sh sqlite <database_file>
#   ./scripts/setup-views.sh postgres [host] [port] [user] [database]
#   ./scripts/setup-views.sh mysql [host] [port] [user] [database]
#   ./scripts/setup-views.sh mssql [host] [port] [user] [database]

set -e  # Exit on error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DB_TYPE="${1:-}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_usage() {
    echo "Usage:"
    echo "  $0 sqlite <database_file>"
    echo "  $0 postgres [host] [port] [user] [database]"
    echo "  $0 mysql [host] [port] [user] [database]"
    echo "  $0 mssql [host] [port] [user] [database]"
    echo ""
    echo "Examples:"
    echo "  $0 sqlite test_data.db"
    echo "  $0 sqlite benchmark_100k.db"
    echo "  $0 postgres localhost 5432 postgres testdb"
    echo "  $0 mysql localhost 3306 root testdb"
    echo "  $0 mssql localhost 1433 sa testdb"
}

setup_sqlite() {
    local db_file="$1"

    if [ -z "$db_file" ]; then
        echo -e "${RED}Error: SQLite database file not specified${NC}"
        print_usage
        exit 1
    fi

    if [ ! -f "$db_file" ]; then
        echo -e "${YELLOW}Warning: Database file '$db_file' does not exist${NC}"
        read -p "Create new database? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
    fi

    echo -e "${GREEN}Setting up views for SQLite: $db_file${NC}"

    # Check if sqlite3 is installed
    if ! command -v sqlite3 &> /dev/null; then
        echo -e "${RED}Error: sqlite3 command not found. Please install SQLite3.${NC}"
        exit 1
    fi

    # Apply SQL script
    sqlite3 "$db_file" < "$SCRIPT_DIR/setup-views-sqlite.sql"

    echo -e "${GREEN}✓ Views created successfully!${NC}"
    echo ""
    echo "Test with:"
    echo "  tdtpcli --config <config.yaml> --list-views"
}

setup_postgres() {
    local host="${1:-localhost}"
    local port="${2:-5432}"
    local user="${3:-postgres}"
    local database="${4:-testdb}"

    echo -e "${GREEN}Setting up views for PostgreSQL${NC}"
    echo "  Host: $host"
    echo "  Port: $port"
    echo "  User: $user"
    echo "  Database: $database"

    # Check if psql is installed
    if ! command -v psql &> /dev/null; then
        echo -e "${YELLOW}Warning: psql command not found${NC}"
        echo "Trying Docker method..."

        # Try Docker
        if command -v docker &> /dev/null; then
            echo "Looking for PostgreSQL Docker container..."
            local container=$(docker ps --filter "ancestor=postgres" --format "{{.Names}}" | head -1)

            if [ -z "$container" ]; then
                echo -e "${RED}Error: No PostgreSQL Docker container found${NC}"
                exit 1
            fi

            echo "Found container: $container"
            docker exec -i "$container" psql -h "$host" -p "$port" -U "$user" -d "$database" < "$SCRIPT_DIR/setup-views-postgres.sql"
        else
            echo -e "${RED}Error: Neither psql nor Docker found${NC}"
            exit 1
        fi
    else
        # Use psql directly
        psql -h "$host" -p "$port" -U "$user" -d "$database" -f "$SCRIPT_DIR/setup-views-postgres.sql"
    fi

    echo -e "${GREEN}✓ Views created successfully!${NC}"
    echo ""
    echo "Test with:"
    echo "  tdtpcli --config postgres_config.yaml --list-views"
}

setup_mysql() {
    local host="${1:-localhost}"
    local port="${2:-3306}"
    local user="${3:-root}"
    local database="${4:-testdb}"

    echo -e "${GREEN}Setting up views for MySQL${NC}"
    echo "  Host: $host"
    echo "  Port: $port"
    echo "  User: $user"
    echo "  Database: $database"

    # Check if mysql is installed
    if ! command -v mysql &> /dev/null; then
        echo -e "${YELLOW}Warning: mysql command not found${NC}"
        echo "Trying Docker method..."

        # Try Docker
        if command -v docker &> /dev/null; then
            echo "Looking for MySQL Docker container..."
            local container=$(docker ps --filter "ancestor=mysql" --format "{{.Names}}" | head -1)

            if [ -z "$container" ]; then
                echo -e "${RED}Error: No MySQL Docker container found${NC}"
                exit 1
            fi

            echo "Found container: $container"
            read -sp "Enter MySQL password: " password
            echo
            docker exec -i "$container" mysql -h "$host" -P "$port" -u "$user" -p"$password" "$database" < "$SCRIPT_DIR/setup-views-mysql.sql"
        else
            echo -e "${RED}Error: Neither mysql nor Docker found${NC}"
            exit 1
        fi
    else
        # Use mysql directly
        read -sp "Enter MySQL password: " password
        echo
        mysql -h "$host" -P "$port" -u "$user" -p"$password" "$database" < "$SCRIPT_DIR/setup-views-mysql.sql"
    fi

    echo -e "${GREEN}✓ Views created successfully!${NC}"
    echo ""
    echo "Test with:"
    echo "  tdtpcli --config mysql_config.yaml --list-views"
}

setup_mssql() {
    local host="${1:-localhost}"
    local port="${2:-1433}"
    local user="${3:-sa}"
    local database="${4:-testdb}"

    echo -e "${GREEN}Setting up views for MS SQL Server${NC}"
    echo "  Host: $host"
    echo "  Port: $port"
    echo "  User: $user"
    echo "  Database: $database"

    # Check if sqlcmd is installed
    if ! command -v sqlcmd &> /dev/null; then
        echo -e "${YELLOW}Warning: sqlcmd command not found${NC}"
        echo "Trying Docker method..."

        # Try Docker
        if command -v docker &> /dev/null; then
            echo "Looking for MS SQL Docker container..."
            local container=$(docker ps --filter "ancestor=mcr.microsoft.com/mssql/server" --format "{{.Names}}" | head -1)

            if [ -z "$container" ]; then
                echo -e "${RED}Error: No MS SQL Docker container found${NC}"
                exit 1
            fi

            echo "Found container: $container"
            read -sp "Enter SQL Server password: " password
            echo
            docker exec -i "$container" /opt/mssql-tools/bin/sqlcmd -S "$host,$port" -U "$user" -P "$password" -d "$database" -i /scripts/setup-views-mssql.sql
        else
            echo -e "${RED}Error: Neither sqlcmd nor Docker found${NC}"
            exit 1
        fi
    else
        # Use sqlcmd directly
        read -sp "Enter SQL Server password: " password
        echo
        sqlcmd -S "$host,$port" -U "$user" -P "$password" -d "$database" -i "$SCRIPT_DIR/setup-views-mssql.sql"
    fi

    echo -e "${GREEN}✓ Views created successfully!${NC}"
    echo ""
    echo "Test with:"
    echo "  tdtpcli --config mssql_config.yaml --list-views"
}

# Main script logic
case "$DB_TYPE" in
    sqlite)
        setup_sqlite "$2"
        ;;
    postgres|postgresql|pg)
        setup_postgres "$2" "$3" "$4" "$5"
        ;;
    mysql)
        setup_mysql "$2" "$3" "$4" "$5"
        ;;
    mssql|sqlserver)
        setup_mssql "$2" "$3" "$4" "$5"
        ;;
    "")
        echo -e "${RED}Error: Database type not specified${NC}"
        print_usage
        exit 1
        ;;
    *)
        echo -e "${RED}Error: Unknown database type '$DB_TYPE'${NC}"
        print_usage
        exit 1
        ;;
esac
