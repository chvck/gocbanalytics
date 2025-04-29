package cbanalytics_test

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cbanalytics "github.com/couchbaselabs/gocbanalytics"
	"github.com/couchbaselabs/gocbanalytics/internal/leakcheck"
)

var TestOpts TestOptions

type TestOptions struct {
	Username        string
	Password        string
	OriginalConnStr string
	Database        string
	Scope           string
}

func TestMain(m *testing.M) {
	var connStr = envFlagString("CBCONNSTR", "connstr", "",
		"Connection string to run tests with")

	var user = envFlagString("CBUSER", "user", "Administrator",
		"The username to use to authenticate when using a real server")

	var password = envFlagString("CBPASS", "pass", "password",
		"The password to use to authenticate when using a real server")

	var database = envFlagString("CBDB", "database", "default",
		"The database to use to authenticate when using a real server")

	var scope = envFlagString("CBSCOPE", "scope", "test",
		"The scope to use to authenticate when using a real server")

	var disableLogger = envFlagBool("CBNOLOG", "disable-logger", false,
		"Whether to disable logging")

	flag.Parse()

	if *connStr == "" {
		panic("connstr cannot be empty")
	}

	if !*disableLogger {
		// Set up our special logger which logs the log level count
		globalTestLogger = createTestLogger()
	}

	TestOpts.OriginalConnStr = *connStr
	TestOpts.Username = *user
	TestOpts.Password = *password
	TestOpts.Database = *database
	TestOpts.Scope = *scope

	leakcheck.EnableAll()

	setupColumnar()

	result := m.Run()

	if globalTestLogger != nil {
		log.Printf("Log Messages Emitted:")

		var preLogTotal uint64

		for i := 0; i < int(cbanalytics.LogTrace+1); i++ {
			count := atomic.LoadUint64(&globalTestLogger.LogCount[i])
			preLogTotal += count
			log.Printf("  (%s): %d", logLevelToString(cbanalytics.LogLevel(i)), count)
		}

		abnormalLogCount := atomic.LoadUint64(&globalTestLogger.LogCount[cbanalytics.LogError]) + atomic.LoadUint64(&globalTestLogger.LogCount[cbanalytics.LogWarn])
		if abnormalLogCount > 0 {
			log.Printf("Detected unexpected logging, failing")

			result = 1
		}

		time.Sleep(1 * time.Second)

		log.Printf("Post sleep log Messages Emitted:")

		var postLogTotal uint64

		for i := 0; i < int(cbanalytics.LogTrace+1); i++ {
			count := atomic.LoadUint64(&globalTestLogger.LogCount[i])
			postLogTotal += count
			log.Printf("  (%s): %d", logLevelToString(cbanalytics.LogLevel(i)), count)
		}

		if preLogTotal != postLogTotal {
			log.Printf("Detected unexpected logging after agent closed, failing")

			result = 1
		}
	}

	if !leakcheck.ReportAll() {
		result = 1
	}

	os.Exit(result)
}

func setupColumnar() {
	cluster, err := cbanalytics.NewCluster(TestOpts.OriginalConnStr, cbanalytics.NewBasicAuthCredential(TestOpts.Username, TestOpts.Password), DefaultOptions())
	if err != nil {
		panic(err)
	}
	defer func(cluster *cbanalytics.Cluster) {
		err := cluster.Close()
		if err != nil {
			panic(err)
		}
	}(cluster)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err = cluster.ExecuteQuery(ctx, fmt.Sprintf("CREATE DATABASE `%s` IF NOT EXISTS", TestOpts.Database))
	if err != nil {
		panic(err)
	}

	_, err = cluster.ExecuteQuery(ctx, fmt.Sprintf("CREATE SCOPE `%s`.`%s` IF NOT EXISTS", TestOpts.Database, TestOpts.Scope))
	if err != nil {
		panic(err)
	}
}

func envFlagBool(envName, name string, value bool, usage string) *bool {
	envValue := os.Getenv(envName)
	if envValue != "" {
		switch {
		case envValue == "0":
			value = false
		case strings.ToLower(envValue) == "false":
			value = false
		default:
			value = true
		}
	}

	return flag.Bool(name, value, usage)
}

func envFlagString(envName, name, value, usage string) *string {
	envValue := os.Getenv(envName)
	if envValue != "" {
		value = envValue
	}

	return flag.String(name, value, usage)
}

var globalTestLogger *testLogger

func DefaultOptions() *cbanalytics.ClusterOptions {
	return cbanalytics.NewClusterOptions().SetSecurityOptions(cbanalytics.NewSecurityOptions().SetDisableServerCertificateVerification(true)).SetLogger(globalTestLogger)
}

type testLogger struct {
	Parent           cbanalytics.Logger
	LogCount         []uint64
	suppressWarnings uint32
}

func (logger *testLogger) Error(format string, v ...interface{}) {
	atomic.AddUint64(&logger.LogCount[cbanalytics.LogError], 1)

	logger.Parent.Error(fmt.Sprintf("[error] %s", format), v...)
}

func (logger *testLogger) Warn(format string, v ...interface{}) {
	if atomic.LoadUint32(&logger.suppressWarnings) == 1 || strings.Contains(format, "server certificate verification is disabled") {
		atomic.AddUint64(&logger.LogCount[cbanalytics.LogInfo], 1)

		logger.Parent.Info(fmt.Sprintf("[info] %s", format), v...)

		return
	}

	atomic.AddUint64(&logger.LogCount[cbanalytics.LogWarn], 1)

	logger.Parent.Warn(fmt.Sprintf("[warn] %s", format), v...)
}

func (logger *testLogger) Info(format string, v ...interface{}) {
	atomic.AddUint64(&logger.LogCount[cbanalytics.LogInfo], 1)

	logger.Parent.Info(fmt.Sprintf("[info] %s", format), v...)
}

func (logger *testLogger) Debug(format string, v ...interface{}) {
	atomic.AddUint64(&logger.LogCount[cbanalytics.LogDebug], 1)

	logger.Parent.Debug(fmt.Sprintf("[debug] %s", format), v...)
}

func (logger *testLogger) Trace(format string, v ...interface{}) {
	atomic.AddUint64(&logger.LogCount[cbanalytics.LogTrace], 1)

	logger.Parent.Trace(fmt.Sprintf("[trace] %s", format), v...)
}

func (logger *testLogger) SuppressWarnings(suppress bool) {
	if suppress {
		atomic.StoreUint32(&logger.suppressWarnings, 1)
	} else {
		atomic.StoreUint32(&logger.suppressWarnings, 0)
	}
}

func createTestLogger() *testLogger {
	return &testLogger{
		Parent:           cbanalytics.NewVerboseLogger(),
		LogCount:         make([]uint64, cbanalytics.LogTrace+1),
		suppressWarnings: 0,
	}
}

func logLevelToString(level cbanalytics.LogLevel) string {
	switch level {
	case cbanalytics.LogError:
		return "error"
	case cbanalytics.LogWarn:
		return "warn"
	case cbanalytics.LogInfo:
		return "info"
	case cbanalytics.LogDebug:
		return "debug"
	case cbanalytics.LogTrace:
		return "trace"
	}

	return fmt.Sprintf("unknown (%d)", level)
}
