#!/bin/bash
# Скрипт для инициализации MSSQL базы данных

echo "Waiting for SQL Server to start..."
sleep 30

echo "Initializing database..."
/opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -P "Tdtp_Pass_123!" -i /init.sql -C

echo "Database initialized!"
