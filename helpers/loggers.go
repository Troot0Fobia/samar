package helpers

import (
	"Troot0Fobia/samar/initializers"
	"time"

	"github.com/sirupsen/logrus"
)

func LogSuccess(msg, username string) {
	initializers.InfoLog.WithFields(logrus.Fields{
		"user": username,
		"time": time.Now().Unix(),
	}).Info(msg)
}

func LogError(msg, username string, err error) {
	initializers.ErrorLog.WithFields(logrus.Fields{
		"error": err,
		"user":  username,
		"time":  time.Now().Unix(),
	}).Error(msg)
}
