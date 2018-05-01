package renter

// TODO expose the downloadCacheSize as a variable and allow users to set it
// via the API.

import (
	"time"

	"github.com/NebulousLabs/errors"
)

// addChunkToCache adds the chunk to the cache if the download is a streaming
// endpoint download.
// TODO this won't be necessary anymore once we have partial downloads.
func (udc *unfinishedDownloadChunk) addChunkToCache(data []byte) {
	if udc.download.staticDestinationType != destinationTypeSeekStream {
		// We only cache streaming chunks since browsers and media players tend
		// to only request a few kib at once when streaming data. That way we can
		// prevent scheduling the same chunk for download over and over.
		return
	}
	udc.cacheMu.Lock()
	defer udc.cacheMu.Unlock()

	// Prune cache if necessary.
	if len(udc.chunkCache) >= downloadCacheSize {
		var oldestKey string
		oldestTime := time.Now().Second()

		for key := range udc.chunkCache {
			if udc.chunkCache[key].timestamp.Second() < oldestTime {
				oldestTime = udc.chunkCache[key].timestamp.Second()
				oldestKey = key
			}
		}
		delete(udc.chunkCache, oldestKey)
	}

	cd := cacheData{
		data:      data,
		timestamp: time.Now(),
	}
	udc.chunkCache[udc.staticCacheID] = cd
}

// managedTryCache tries to retrieve the chunk from the renter's cache. If
// successful it will write the data to the destination and stop the download
// if it was the last missing chunk. The function returns true if the chunk was
// in the cache.
// TODO in the future we might need cache invalidation. At the
// moment this doesn't worry us since our files are static.
func (r *Renter) managedTryCache(udc *unfinishedDownloadChunk) bool {
	udc.mu.Lock()
	defer udc.mu.Unlock()
	r.cmu.Lock()
	cd, cached := r.chunkCache[udc.staticCacheID]
	data := cd.data
	r.cmu.Unlock()
	if !cached {
		return false
	}
	start := udc.staticFetchOffset
	end := start + udc.staticFetchLength
	_, err := udc.destination.WriteAt(data[start:end], udc.staticWriteOffset)
	if err != nil {
		r.log.Println("WARN: failed to write cached chunk to destination:", err)
		udc.fail(errors.AddContext(err, "failed to write cached chunk to destination"))
		return true
	}

	// Check if the download is complete now.
	udc.download.mu.Lock()
	udc.download.chunksRemaining--
	if udc.download.chunksRemaining == 0 {
		udc.download.endTime = time.Now()
		close(udc.download.completeChan)
	}
	udc.download.mu.Unlock()
	return true
}
