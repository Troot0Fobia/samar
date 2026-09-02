package initializers

import (
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var InfoLog = logrus.New()
var ErrorLog = logrus.New()

func InitLogger() {
	InfoLog.SetOutput(&lumberjack.Logger{
		Filename:   "./logs/access.log",
		MaxSize:    50, // MB per file before rotating
		MaxBackups: 5,  // rotated files to keep
		MaxAge:     14, // days before a rotated file is deleted
		Compress:   true,
	})
	InfoLog.SetFormatter(&logrus.JSONFormatter{})
	InfoLog.SetLevel(logrus.InfoLevel)
	InfoLog.SetReportCaller(true)

	ErrorLog.SetOutput(&lumberjack.Logger{
		Filename:   "./logs/error.log",
		MaxSize:    50,
		MaxBackups: 5,
		MaxAge:     14,
		Compress:   true,
	})
	ErrorLog.SetFormatter(&logrus.JSONFormatter{})
	ErrorLog.SetLevel(logrus.ErrorLevel)
	ErrorLog.SetReportCaller(true)
}
