package repo

import (
	"bytes"
	"fmt"
	"io"

	"github.com/kopia/kopia/storage"
)

// ObjectWriter allows writing content to the storage and supports automatic deduplication and encryption
// of written data.
type ObjectWriter interface {
	io.WriteCloser

	Result(forceStored bool) (ObjectID, error)
}

type objectWriter struct {
	repo *repository

	buffer      *bytes.Buffer
	totalLength int64

	prefix             string
	listWriter         *objectWriter
	flushedObjectCount int
	lastFlushedObject  ObjectID

	description string
	objectType  ObjectIDType

	atomicWrites bool
}

func (w *objectWriter) Close() error {
	if w.buffer != nil {
		w.repo.bufferManager.returnBuffer(w.buffer)
		w.buffer = nil
	}

	if w.listWriter != nil {
		w.listWriter.Close()
		w.listWriter = nil
	}
	return nil
}

func (w *objectWriter) Write(data []byte) (n int, err error) {
	dataLen := len(data)
	remaining := len(data)
	w.totalLength += int64(remaining)

	for remaining > 0 {
		if w.buffer == nil {
			w.buffer = w.repo.bufferManager.newBuffer()
		}
		room := w.buffer.Cap() - w.buffer.Len()

		if remaining <= room {
			w.buffer.Write(data)
			remaining = 0
		} else {
			if w.atomicWrites {
				if w.buffer == nil {
					// We're at the beginning of a buffer, fail if the buffer is too small.
					return 0, fmt.Errorf("object writer buffer too small, need: %v, have: %v", remaining, room)
				}
				if err := w.flushBuffer(false); err != nil {
					return 0, err
				}

				continue
			}

			w.buffer.Write(data[0:room])

			if err := w.flushBuffer(false); err != nil {
				return 0, err
			}
			data = data[room:]
			remaining = len(data)
		}
	}
	return dataLen, nil
}

func (w *objectWriter) flushBuffer(force bool) error {
	// log.Printf("flushing bufer")
	// defer log.Printf("flushed")
	if w.buffer != nil || force {
		var length int
		if w.buffer != nil {
			length = w.buffer.Len()
		}

		b := w.buffer
		w.buffer = nil
		objectID, blockReader, err := w.repo.hashBufferForWriting(b, string(w.objectType)+w.prefix)
		if err != nil {
			return err
		}

		if err := w.repo.storage.PutBlock(objectID.BlockID(), blockReader, storage.PutOptionsDefault); err != nil {
			return fmt.Errorf(
				"error when flushing chunk %d of %s to %#v: %#v",
				w.flushedObjectCount,
				w.description,
				objectID.BlockID(),
				err)
		}

		w.flushedObjectCount++
		w.lastFlushedObject = objectID
		if w.listWriter == nil {
			w.listWriter = newObjectWriter(w.repo, ObjectIDTypeList)
			w.listWriter.prefix = w.prefix
			w.listWriter.description = "LIST(" + w.description + ")"
			w.listWriter.atomicWrites = true
		}

		fmt.Fprintf(w.listWriter, "%v,%v\n", length, objectID)
	}
	return nil
}

func newObjectWriter(repo *repository, objectType ObjectIDType) *objectWriter {
	return &objectWriter{
		repo:       repo,
		objectType: objectType,
	}
}

func (w *objectWriter) Result(forceStored bool) (ObjectID, error) {
	if !forceStored && w.flushedObjectCount == 0 {
		if w.buffer == nil {
			return "B", nil
		}

		if w.buffer.Len() < w.repo.format.MaxInlineBlobSize {
			return NewInlineObjectID(w.buffer.Bytes()), nil
		}
	}

	w.flushBuffer(forceStored)
	defer func() {
		if w.listWriter != nil {
			w.listWriter.Close()
		}
	}()

	if w.flushedObjectCount == 1 {
		return w.lastFlushedObject, nil
	} else if w.flushedObjectCount == 0 {
		return "", nil
	} else {
		return w.listWriter.Result(true)
	}
}

// WriterOption is an option that can be passed to Repository.NewWriter()
type WriterOption func(*objectWriter)

// WithBlockNamePrefix causes the ObjectWriter to prefix any blocks emitted to the storage with a given string.
func WithBlockNamePrefix(prefix string) WriterOption {
	return func(w *objectWriter) {
		w.prefix = prefix
	}
}

// WithDescription is used for debugging only and causes the following string to be emitted in logs.
func WithDescription(description string) WriterOption {
	return func(w *objectWriter) {
		w.description = description
	}
}