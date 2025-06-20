package logger

import (
    "time"

    "github.com/natefinch/lumberjack"
    logrus "github.com/sirupsen/logrus"
)

// Setup initializes Logrus and GORM logging via a rotating file.
func Setup() {
    // 1) Lumberjack for file rotation
    rotator := &lumberjack.Logger{
        Filename:   "./logs/app.log",
        MaxSize:    10,  // megabytes
        MaxBackups: 7,   // keep up to 7 old files
        MaxAge:     7,   // days
        Compress:   true,
    }

    // 2) Configure Logrus to write to that file
    logrus.SetOutput(rotator)
    logrus.SetFormatter(&logrus.TextFormatter{
        FullTimestamp:   true,
        TimestampFormat: time.RFC3339,
    })
    logrus.SetLevel(logrus.DebugLevel) // capture SQL at Debug/Info
}

// GormLogger returns the standard Logrus logger for GORM
func GormLogger() *logrus.Logger {
    return logrus.StandardLogger()
}
