package cbanalytics_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	cbanalytics "github.com/couchbaselabs/gocbanalytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicQuery(t *testing.T) {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr, cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password), DefaultOptions())
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	ExecuteQueryAgainst(t, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(tt *testing.T, queryable Queryable) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res, err := queryable.ExecuteQuery(ctx, "FROM RANGE(0, 99) AS i SELECT RAW i")
		require.NoError(tt, err)

		actualRows := CollectRows[int](t, res)
		require.Len(tt, actualRows, 100)

		for i := 0; i < 100; i++ {
			require.Equal(tt, i, actualRows[i])
		}

		err = res.Err()
		require.NoError(tt, err)

		meta, err := res.MetaData()
		require.NoError(tt, err)

		assertMeta(tt, meta, 100)
	})
}

func TestBasicBufferedQuery(t *testing.T) {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr, cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password), DefaultOptions())
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	ExecuteQueryAgainst(t, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(tt *testing.T, queryable Queryable) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res, err := queryable.ExecuteQuery(ctx, "FROM RANGE(0, 99) AS i SELECT RAW i")
		require.NoError(tt, err)

		actualRows, meta, err := cbanalytics.BufferQueryResult[int](res)
		require.NoError(tt, err)

		for i := 0; i < 100; i++ {
			require.Equal(tt, i, actualRows[i])
		}

		assertMeta(tt, meta, 100)
	})
}

func TestOperationTimeout(t *testing.T) {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr,
		cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password),
		DefaultOptions(),
	)
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	t.Run("Context Deadline", func(tt *testing.T) {
		ExecuteQueryAgainst(tt, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(ttt *testing.T, queryable Queryable) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			_, err := queryable.ExecuteQuery(ctx, "SELECT sleep('foo', 5000);")
			require.ErrorIs(ttt, err, context.DeadlineExceeded)

			var columnarErr *cbanalytics.ColumnarError

			require.ErrorAs(ttt, err, &columnarErr)

			assert.NotContains(ttt, columnarErr.Error(), "operation not sent to server")
		})
	})

	t.Run("Context Cancel", func(tt *testing.T) {
		ExecuteQueryAgainst(tt, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(ttt *testing.T, queryable Queryable) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			go func() {
				time.Sleep(1 * time.Second)
				cancel()
			}()

			_, err := queryable.ExecuteQuery(ctx, "SELECT sleep('foo', 5000);")
			require.ErrorIs(ttt, err, context.Canceled)

			var columnarErr *cbanalytics.ColumnarError

			require.ErrorAs(ttt, err, &columnarErr)

			assert.NotContains(ttt, columnarErr.Error(), "operation not sent to server")
		})
	})

	t.Run("Timeout", func(tt *testing.T) {
		cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr,
			cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password),
			DefaultOptions().SetTimeoutOptions(cbanalytics.NewTimeoutOptions().SetQueryTimeout(1*time.Second)),
		)
		require.NoError(tt, err)
		defer func(cluster *cbanalytics.Cluster) {
			err := cluster.Close()
			assert.NoError(tt, err)
		}(cluster)

		ExecuteQueryAgainst(tt, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(ttt *testing.T, queryable Queryable) {
			ctx := context.Background()

			_, err := queryable.ExecuteQuery(ctx, "SELECT sleep('foo', 5000);")
			require.ErrorIs(ttt, err, cbanalytics.ErrTimeout)

			var columnarErr *cbanalytics.ColumnarError

			require.ErrorAs(ttt, err, &columnarErr)

			assert.NotContains(ttt, columnarErr.Error(), "operation not sent to server")
		})
	})
}

func TestQueryError(t *testing.T) {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr,
		cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password),
		DefaultOptions(),
	)
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	ExecuteQueryAgainst(t, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(tt *testing.T, queryable Queryable) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := queryable.ExecuteQuery(ctx, "SELEC 123;")
		require.ErrorIs(tt, err, cbanalytics.ErrQuery)

		var columnarErr *cbanalytics.ColumnarError

		require.ErrorAs(tt, err, &columnarErr)

		var queryErr *cbanalytics.QueryError

		require.ErrorAs(tt, err, &queryErr)

		assert.Equal(tt, 24000, queryErr.Code())
		assert.NotEmpty(tt, queryErr.Message())
	})
}

func TestInvalidCredential(t *testing.T) {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr,
		cbanalytics.NewBasicAuthCredential(TestOpts.Username, "prettyunlikelytobeapassword!"),
		DefaultOptions(),
	)
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	ExecuteQueryAgainst(t, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(tt *testing.T, queryable Queryable) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := queryable.ExecuteQuery(ctx, "SELECT 123;")
		require.ErrorIs(tt, err, cbanalytics.ErrInvalidCredential)

		var columnarErr *cbanalytics.ColumnarError

		require.ErrorAs(tt, err, &columnarErr)
	})
}

func TestUnmarshaler(t *testing.T) {
	unmarshaler := &ErrorUnmarshaler{
		Err: errors.New("something went wrong"), // nolint: err113
	}

	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr,
		cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password),
		DefaultOptions().SetUnmarshaler(unmarshaler),
	)
	require.NoError(t, err)
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		assert.NoError(t, err)
	}(cluster)

	ExecuteQueryAgainst(t, []Queryable{cluster, cluster.Database(TestOpts.Database).Scope(TestOpts.Scope)}, func(tt *testing.T, queryable Queryable) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res, err := queryable.ExecuteQuery(ctx, "FROM RANGE(0, 1) AS i SELECT RAW i")
		require.NoError(tt, err)

		for row := res.NextRow(); row != nil; row = res.NextRow() {
			var val interface{}
			err = row.ContentAs(&val)
			require.ErrorIs(tt, err, unmarshaler.Err)
		}
	})
}

type ErrorUnmarshaler struct {
	Err error
}

func (e *ErrorUnmarshaler) Unmarshal(_ []byte, _ interface{}) error {
	return e.Err
}

func assertMeta(t *testing.T, meta *cbanalytics.QueryMetadata, resultCount uint64) {
	assert.Empty(t, meta.Warnings)
	assert.NotEmpty(t, meta.RequestID)

	assert.NotZero(t, meta.Metrics.ElapsedTime)
	assert.NotZero(t, meta.Metrics.ExecutionTime)
	assert.NotZero(t, meta.Metrics.ResultSize)
	assert.Equal(t, resultCount, meta.Metrics.ResultCount)
	assert.Zero(t, meta.Metrics.ProcessedObjects)
}

type Queryable interface {
	ExecuteQuery(ctx context.Context, statement string, opts ...*cbanalytics.QueryOptions) (*cbanalytics.QueryResult, error)
}

func ExecuteQueryAgainst(t *testing.T, queryables []Queryable, fn func(tt *testing.T, queryable Queryable)) {
	for _, queryable := range queryables {
		t.Run(reflect.TypeOf(queryable).Elem().String(), func(tt *testing.T) {
			fn(tt, queryable)
		})
	}
}

func CollectRows[T any](t *testing.T, res *cbanalytics.QueryResult) []T {
	var actualRows []T

	for row := res.NextRow(); row != nil; row = res.NextRow() {
		var actualRow T
		err := row.ContentAs(&actualRow)
		require.NoError(t, err)

		actualRows = append(actualRows, actualRow)
	}

	return actualRows
}
