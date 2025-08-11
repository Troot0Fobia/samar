package initializers

import (
	"log"
	"os"

	"github.com/sirupsen/logrus"
)

var InfoLog = logrus.New()
var ErrorLog = logrus.New()

func InitLogger() {
	infoFile, err := os.OpenFile("./logs/access.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	InfoLog.SetOutput(infoFile)
	InfoLog.SetFormatter(&logrus.JSONFormatter{})
	InfoLog.SetLevel(logrus.InfoLevel)
	InfoLog.SetReportCaller(true)

	errorFile, err := os.OpenFile("./logs/error.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	ErrorLog.SetOutput(errorFile)
	ErrorLog.SetFormatter(&logrus.JSONFormatter{})
	ErrorLog.SetLevel(logrus.ErrorLevel)
	InfoLog.SetReportCaller(true)
}
