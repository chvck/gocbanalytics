package httpqueryclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/couchbase/gocbanalytics/internal/logging"
)

// QueryResult allows access to the results of a N1QL query.
type queryStreamer struct {
	metaDataBytes []byte
	err           error
	lock          sync.Mutex

	stream   io.ReadCloser
	streamer *rowStreamer
	logger   logging.Logger
}

func newQueryStreamer(stream io.ReadCloser, logger logging.Logger, rowsAttrib string) (*queryStreamer, error) {
	rowStreamer, err := newRowStreamer(stream, rowsAttrib)
	if err != nil {
		closeErr := stream.Close()
		if closeErr != nil {
			logger.Debug("query stream close failed after error: %s", closeErr)
		}

		return nil, err
	}

	return &queryStreamer{
		metaDataBytes: nil,
		err:           nil,
		lock:          sync.Mutex{},
		stream:        stream,
		streamer:      rowStreamer,
		logger:        logger,
	}, nil
}

// NextRow returns the next row from the results, returning nil when the rows are exhausted.
func (r *queryStreamer) NextRow() []byte {
	if r.streamer == nil {
		return nil
	}

	rowBytes, err := r.streamer.NextRowBytes()
	if err != nil {
		r.finishWithError(err)

		return nil
	}

	// Check if there were any rows left
	if rowBytes == nil {
		r.finishWithoutError()

		return nil
	}

	return rowBytes
}

// Err returns any errors that have occurred on the stream
func (r *queryStreamer) Err() error {
	r.lock.Lock()
	err := r.err
	r.lock.Unlock()

	return err
}

// EarlyMetadata returns the value (or nil) of an attribute from a query metadata before the query has completed.
func (r *queryStreamer) EarlyMetadata(key string) json.RawMessage {
	return r.streamer.EarlyAttrib(key)
}

func (r *queryStreamer) finishWithoutError() {
	// Let's finalize the streamer so we Get the meta-data
	metaDataBytes, err := r.streamer.Finalize()
	if err != nil {
		r.finishWithError(err)

		return
	}

	// Streamer is no longer valid now that it's been Finalized
	r.streamer = nil

	// Close the stream now that we are done with it
	err = r.stream.Close()
	if err != nil {
		r.logger.Warn("query stream close failed after meta-data: %s", err)
	}

	// The stream itself is no longer valid
	r.lock.Lock()
	r.stream = nil
	r.lock.Unlock()

	r.metaDataBytes = metaDataBytes
}

func (r *queryStreamer) finishWithError(err error) {
	// Let's record the error that happened
	r.err = err

	// Our streamer is invalidated as soon as an error occurs
	r.streamer = nil

	// Let's close the underlying stream
	closeErr := r.stream.Close()
	if closeErr != nil {
		// We log this at debug level, but its almost always going to be an
		// error since thats the most likely reason we are in finishWithError
		r.logger.Debug("query stream close failed after error: %s", closeErr)
	}

	// The stream itself is now no longer valid
	r.stream = nil
}

// Close marks the results as closed, returning any errors that occurred during reading the results.
func (r *queryStreamer) Close() error {
	// If an error occurred before, we should return that (forever)
	err := r.Err()
	if err != nil {
		return err
	}

	r.lock.Lock()
	stream := r.stream
	r.lock.Unlock()

	// If the stream is already closed, we can imply that no error occurred
	if stream == nil {
		return nil
	}

	err = stream.Close()
	if err != nil {
		return fmt.Errorf("failed to close stream: %w", err)
	}

	return nil
}

// One assigns the first value from the results into the value pointer.
// It will close the results but not before iterating through all remaining
// results, as such this should only be used for very small resultsets - ideally
// of, at most, length 1.
func (r *queryStreamer) One() ([]byte, error) {
	rowBytes := r.NextRow()
	if rowBytes == nil {
		if r.Err() == nil {
			return nil, errors.New("no rows available") //nolint:err113
		}

		return nil, r.Close()
	}

	// Read any remaining rows
	for r.NextRow() != nil { //nolint:revive
		// skip
	}

	// If an error occurred during the streaming, we need to
	// return that, and make sure the result is closed
	err := r.Err()
	if err != nil {
		return nil, err
	}

	return rowBytes, nil
}

func (r *queryStreamer) MetaData() ([]byte, error) {
	if r.streamer != nil {
		return nil, errors.New("the result must be closed before accessing the meta-data") //nolint:err113
	}

	if r.metaDataBytes == nil {
		return nil, errors.New("an error occurred during querying which has made the meta-data unavailable") //nolint:err113
	}

	return r.metaDataBytes, nil
}
