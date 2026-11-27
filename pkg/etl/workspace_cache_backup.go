package etl

import (
	"context"
	"fmt"

	"modernc.org/sqlite"
)

// Восстановление базы из файла идёт через online backup API SQLite: страницы
// копируются как есть, вместе с индексами.
//
// Deserialize драйвера не используется: в modernc v1.46.0 он берёт буфер через
// tls.Alloc и отдаёт его SQLite с флагом FREEONCLOSE. Serialize такого не
// делает — копирует образ в память Go и освобождает буфер SQLite сам.

type sqliteBackuper interface {
	NewBackup(dstURI string) (*sqlite.Backup, error)
	NewRestore(srcURI string) (*sqlite.Backup, error)
}

func (w *Workspace) backupToFile(ctx context.Context, path string) error {
	return w.runBackup(ctx, func(c sqliteBackuper) (*sqlite.Backup, error) { return c.NewBackup(path) })
}

func (w *Workspace) restoreFromFile(ctx context.Context, path string) error {
	return w.runBackup(ctx, func(c sqliteBackuper) (*sqlite.Backup, error) { return c.NewRestore(path) })
}

func (w *Workspace) runBackup(ctx context.Context, start func(sqliteBackuper) (*sqlite.Backup, error)) error {
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	return conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(sqliteBackuper)
		if !ok {
			return fmt.Errorf("sqlite driver does not support the backup API")
		}
		backup, err := start(backuper)
		if err != nil {
			return err
		}

		// Порциями по 4096 страниц, чтобы отмена контекста не ждала всей базы.
		more := true
		for more && err == nil {
			if err = ctx.Err(); err == nil {
				more, err = backup.Step(4096)
			}
		}
		if finishErr := backup.Finish(); err == nil {
			err = finishErr
		}
		return err
	})
}

// serialize возвращает образ базы в памяти.
func (w *Workspace) serialize(ctx context.Context) ([]byte, error) {
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	var image []byte
	err = conn.Raw(func(driverConn any) error {
		serializer, ok := driverConn.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return fmt.Errorf("sqlite driver does not support Serialize")
		}
		var serializeErr error
		image, serializeErr = serializer.Serialize()
		return serializeErr
	})
	return image, err
}
