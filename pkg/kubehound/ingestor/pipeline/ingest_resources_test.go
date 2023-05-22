package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	collector "github.com/DataDog/KubeHound/pkg/collector/mocks"
	"github.com/DataDog/KubeHound/pkg/globals/types"
	"github.com/DataDog/KubeHound/pkg/kubehound/graph/vertex"
	"github.com/DataDog/KubeHound/pkg/kubehound/models/converter"
	cache "github.com/DataDog/KubeHound/pkg/kubehound/storage/cache/mocks"
	graphdb "github.com/DataDog/KubeHound/pkg/kubehound/storage/graphdb/mocks"
	storedb "github.com/DataDog/KubeHound/pkg/kubehound/storage/storedb/mocks"
	"github.com/DataDog/KubeHound/pkg/kubehound/store/collections"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Shared function to load test objects across all ingests
func loadTestObject[T types.InputType](filename string) (T, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	var output T
	err = decoder.Decode(&output)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func TestIngestResources_Initializer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := collector.NewCollectorClient(t)
	c := cache.NewCacheProvider(t)
	gdb := graphdb.NewProvider(t)
	sdb := storedb.NewProvider(t)

	deps := &Dependencies{
		Collector: client,
		Cache:     c,
		GraphDB:   gdb,
		StoreDB:   sdb,
	}

	// Test default initialization
	oi, err := CreateResources(ctx, deps)
	assert.NoError(t, err)
	assert.IsType(t, &collector.CollectorClient{}, oi.collect)
	assert.IsType(t, &converter.StoreConverter{}, oi.storeConvert)
	assert.IsType(t, &converter.GraphConverter{}, oi.graphConvert)
	assert.Equal(t, 0, len(oi.cleanup))
	assert.Equal(t, 0, len(oi.flush))

	// Test cache writer mechanics
	oi = &IngestResources{}
	cw := cache.NewAsyncWriter(t)
	cwDone := make(chan struct{})
	cw.EXPECT().Flush(mock.Anything).Return(cwDone, nil)
	cw.EXPECT().Close(mock.Anything).Return(nil)

	c.EXPECT().BulkWriter(mock.Anything).Return(cw, nil)

	oi, err = CreateResources(ctx, deps, WithCacheWriter())
	assert.NoError(t, err)

	close(cwDone)
	assert.NoError(t, oi.flushWriters(ctx))
	assert.NoError(t, oi.cleanupAll(ctx))

	// Test store writer mechanics
	oi = &IngestResources{}
	sw := storedb.NewAsyncWriter(t)
	swDone := make(chan struct{})
	sw.EXPECT().Flush(mock.Anything).Return(swDone, nil)
	sw.EXPECT().Close(mock.Anything).Return(nil)

	collection := collections.Node{}
	sdb.EXPECT().BulkWriter(mock.Anything, collection).Return(sw, nil)

	oi, err = CreateResources(ctx, deps, WithStoreWriter(collection))
	assert.NoError(t, err)

	close(swDone)
	assert.NoError(t, oi.flushWriters(ctx))
	assert.NoError(t, oi.cleanupAll(ctx))

	// Test graph writer mechanics
	oi = &IngestResources{}
	gw := graphdb.NewAsyncVertexWriter(t)
	gwDone := make(chan struct{})
	gw.EXPECT().Flush(mock.Anything).Return(gwDone, nil)
	gw.EXPECT().Close(mock.Anything).Return(nil)

	vtx := vertex.Node{}
	gdb.EXPECT().VertexWriter(mock.Anything, mock.AnythingOfType("vertex.VertexTraversal")).Return(gw, nil)

	oi, err = CreateResources(ctx, deps, WithGraphWriter(vtx))
	assert.NoError(t, err)

	close(gwDone)
	assert.NoError(t, oi.flushWriters(ctx))
	assert.NoError(t, oi.cleanupAll(ctx))
}

func TestIngestResources_FlushErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oi := &IngestResources{}

	client := collector.NewCollectorClient(t)
	c := cache.NewCacheProvider(t)
	gdb := graphdb.NewProvider(t)
	sdb := storedb.NewProvider(t)

	deps := &Dependencies{
		Collector: client,
		Cache:     c,
		GraphDB:   gdb,
		StoreDB:   sdb,
	}

	// Set cache to succeed
	cw := cache.NewAsyncWriter(t)
	cwDone := make(chan struct{})
	cw.EXPECT().Flush(mock.Anything).Return(cwDone, nil)
	c.EXPECT().BulkWriter(mock.Anything).Return(cw, nil)

	// Set store to fail
	sw := storedb.NewAsyncWriter(t)
	swDone := make(chan struct{})
	sw.EXPECT().Flush(mock.Anything).Return(swDone, errors.New("test error"))
	sdb.EXPECT().BulkWriter(mock.Anything, mock.Anything).Return(sw, nil)

	oi, err := CreateResources(ctx, deps, WithCacheWriter(), WithStoreWriter(collections.Node{}))
	assert.NoError(t, err)

	close(cwDone)
	close(swDone)
	assert.ErrorContains(t, oi.flushWriters(ctx), "test error")
}

func TestIngestResources_CloseErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oi := &IngestResources{}

	client := collector.NewCollectorClient(t)
	c := cache.NewCacheProvider(t)
	gdb := graphdb.NewProvider(t)
	sdb := storedb.NewProvider(t)

	deps := &Dependencies{
		Collector: client,
		Cache:     c,
		GraphDB:   gdb,
		StoreDB:   sdb,
	}

	// Set cache to succeed
	cw := cache.NewAsyncWriter(t)
	cw.EXPECT().Close(mock.Anything).Return(nil)
	c.EXPECT().BulkWriter(mock.Anything).Return(cw, nil)

	// Set store to fail
	sw := storedb.NewAsyncWriter(t)
	sw.EXPECT().Close(mock.Anything).Return(errors.New("test error"))
	sdb.EXPECT().BulkWriter(mock.Anything, mock.Anything).Return(sw, nil)

	oi, err := CreateResources(ctx, deps, WithCacheWriter(), WithStoreWriter(collections.Node{}))
	assert.NoError(t, err)

	assert.ErrorContains(t, oi.cleanupAll(ctx), "test error")
}