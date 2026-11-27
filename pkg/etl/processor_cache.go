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
// Возвращает данные источников для applySchemaPassthrough. В пакетах TDTP-файлов
// и всего прочитанного из кеша есть только схема и число строк.
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

	if keyErr == nil {
		p.saveCache(ctx, key, sourcesData)
	}

	return sourcesData, nil
}

// saveCache сохраняет рабочую БД под ключом key, посчитанным до загрузки.
// Ошибки не возвращает намеренно: без кеша пайплайн всё равно даёт тот же
// результат, поэтому неудача сохранения — только предупреждение.
func (p *Processor) saveCache(ctx context.Context, key string, sourcesData []SourceData) {
	cache := p.config.Workspace.Cache

	// Файл мог измениться, пока мы его читали: тогда в базе новые данные под
	// старым ключом.
	after, err := workspaceCacheKey(p.config.Sources, cache.Indexes)
	if err != nil {
		fmt.Printf("WARNING: workspace cache not saved: %v\n", err)
		return
	}
	if after != key {
		fmt.Println("WARNING: workspace cache not saved: source files changed during loading")
		return
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

	started := time.Now()
	if err := p.workspace.saveWorkspaceCache(ctx, cache.Path, header); err != nil {
		fmt.Printf("WARNING: workspace cache not saved: %v\n", err)
		return
	}
	p.stats.CacheSaved = true
	fmt.Printf("Workspace cache: saved %s in %s\n", cache.Path, time.Since(started).Round(time.Millisecond))
}

// loadAndPopulate наполняет workspace из источников. Локальные TDTP-файлы
// грузятся по частям (loadByPart), остальные источники — целиком.
//
// Любая ошибка обрывает пайплайн, так что наполовину заполненная таблица до
// transform не доходит.
func (p *Processor) loadAndPopulate(ctx context.Context) ([]SourceData, error) {
	var whole, byPart []SourceConfig
	for _, source := range p.config.Sources {
		if source.Type == "tdtp" {
			byPart = append(byPart, source)
		} else {
			whole = append(whole, source)
		}
	}

	var sourcesData []SourceData
	// Пустой список источников доходит до loadSources ради прежней ошибки.
	if len(whole) > 0 || len(byPart) == 0 {
		loaded, err := p.loadSources(ctx, whole)
		if err != nil {
			return nil, fmt.Errorf("failed to load sources: %w", err)
		}
		if err := p.populateWorkspace(ctx, loaded); err != nil {
			return nil, fmt.Errorf("failed to populate workspace: %w", err)
		}
		sourcesData = loaded
	}

	for _, source := range byPart {
		pkt, err := p.loadByPart(ctx, source)
		if err != nil {
			return nil, err
		}
		sourcesData = append(sourcesData, SourceData{SourceName: source.Name, TableName: source.Name, Packet: pkt})
		p.stats.SourcesLoaded++
		p.stats.TotalRowsLoaded += pkt.Header.RecordsInPart
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
