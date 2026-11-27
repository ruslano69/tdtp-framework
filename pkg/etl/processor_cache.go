package etl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// prepareWorkspace наполняет рабочую БД: из кеша, если он задан и актуален,
// иначе из источников, после чего строит индексы и сохраняет кеш.
//
// Неисправность самого кеша пайплайн не роняет: он работает как без кеша и
// пишет предупреждение. Ошибка в списке индексов — это ошибка конфига.
//
// Возвращает данные источников для applySchemaPassthrough. После чтения из кеша
// в пакетах есть только схема и число строк.
func (p *Processor) prepareWorkspace(ctx context.Context) ([]SourceData, error) {
	cache := p.config.Workspace.Cache
	if cache == nil {
		return p.loadAndPopulate(ctx)
	}

	started := time.Now()
	key, keyErr := workspaceCacheKey(p.config.Sources, cache.Indexes)
	if keyErr != nil {
		// Скорее всего, нет файла источника: обычная загрузка сообщит об этом
		// по правилам on_source_error.
		fmt.Printf("WARNING: workspace cache not used: %v\n", keyErr)
	} else {
		header, err := p.workspace.loadWorkspaceCache(ctx, cache.Path, key)
		switch {
		case err == nil:
			p.stats.CacheHit = true
			fmt.Printf("Workspace cache: loaded %s in %s\n", cache.Path, time.Since(started).Round(time.Millisecond))
			return p.sourcesFromCache(header), nil
		case errors.Is(err, errCacheStale):
			fmt.Printf("Workspace cache: no up-to-date cache at %s, loading sources\n", cache.Path)
		default:
			fmt.Printf("WARNING: workspace cache %s is unusable, loading sources: %v\n", cache.Path, err)
			// Восстановление могло оборваться на середине.
			if err := p.resetWorkspace(ctx); err != nil {
				return nil, err
			}
		}
	}

	sourcesData, err := p.loadAndPopulate(ctx)
	if err != nil {
		return nil, err
	}

	if err := p.workspace.CreateIndexes(ctx, cache.Indexes); err != nil {
		return nil, fmt.Errorf("workspace.cache.indexes: %w", err)
	}

	if keyErr != nil {
		return sourcesData, nil
	}

	// Файл мог измениться, пока мы его читали: тогда в базе новые данные под
	// старым ключом.
	if after, err := workspaceCacheKey(p.config.Sources, cache.Indexes); err != nil || after != key {
		fmt.Println("WARNING: workspace cache not saved: source files changed during loading")
		return sourcesData, nil
	}

	header := workspaceCacheHeader{Key: key}
	for _, source := range sourcesData {
		header.Sources = append(header.Sources, workspaceCachedSource{
			Name:   source.SourceName,
			Table:  source.TableName,
			Rows:   source.Packet.Header.RecordsInPart,
			Schema: source.Packet.Schema,
		})
	}

	started = time.Now()
	if err := p.workspace.saveWorkspaceCache(ctx, cache.Path, header); err != nil {
		fmt.Printf("WARNING: workspace cache not saved: %v\n", err)
		return sourcesData, nil
	}
	p.stats.CacheSaved = true
	fmt.Printf("Workspace cache: saved %s in %s\n", cache.Path, time.Since(started).Round(time.Millisecond))

	return sourcesData, nil
}

func (p *Processor) loadAndPopulate(ctx context.Context) ([]SourceData, error) {
	sourcesData, err := p.loadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load sources: %w", err)
	}
	if err := p.populateWorkspace(ctx, sourcesData); err != nil {
		return nil, fmt.Errorf("failed to populate workspace: %w", err)
	}
	return sourcesData, nil
}

func (p *Processor) sourcesFromCache(header workspaceCacheHeader) []SourceData {
	sourcesData := make([]SourceData, 0, len(header.Sources))
	for _, cached := range header.Sources {
		pkt := packet.NewDataPacket(packet.TypeReference, cached.Table)
		pkt.Schema = cached.Schema
		pkt.Header.RecordsInPart = cached.Rows
		sourcesData = append(sourcesData, SourceData{SourceName: cached.Name, TableName: cached.Table, Packet: pkt})
		p.stats.TotalRowsLoaded += cached.Rows
	}
	p.stats.SourcesLoaded = len(sourcesData)
	return sourcesData
}

func (p *Processor) resetWorkspace(ctx context.Context) error {
	if err := p.workspace.Close(ctx); err != nil {
		return fmt.Errorf("close workspace: %w", err)
	}
	workspace, err := NewWorkspace(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}
	p.workspace = workspace
	p.executor = NewExecutor(workspace)
	return nil
}
