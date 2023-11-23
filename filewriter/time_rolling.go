package filewriter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RollingPeriod defines the enumeration for file rolling periods
type RollingPeriod string

const (
	RollingPeriodYear   RollingPeriod = "YEAR"
	RollingPeriodMonth  RollingPeriod = "MONTH"
	RollingPeriodDay    RollingPeriod = "DAY"
	RollingPeriodHour   RollingPeriod = "HOUR"
	RollingPeriodMinute RollingPeriod = "MINUTE"
	RollingPeriodSecond RollingPeriod = "SECOND"
)

// TimeRollingFileWriter is a time-based rolling file writer
type TimeRollingFileWriter struct {
	mu              sync.Mutex
	nextCheckTime   time.Time
	deleteCheckTime time.Time
	file            *os.File

	basePath       string
	baseFilePrefix string
	baseFileExt    string
	maxBackups     int
	rollPeriod     RollingPeriod
}

// NewTimeRollingFileWriter creates a new instance of TimeRollingFileWriter
func NewTimeRollingFileWriter(
	basePath, baseFileName string,
	maxBackups int,
	rollPeriod RollingPeriod,
) (*TimeRollingFileWriter, error) {
	if err := os.MkdirAll(basePath, os.ModePerm); err != nil {
		return nil, err
	}
	w := &TimeRollingFileWriter{}
	if maxBackups < 0 {
		maxBackups = 0
	}
	w.basePath = basePath
	w.maxBackups = maxBackups
	w.baseFileExt = filepath.Ext(baseFileName)
	w.baseFilePrefix = strings.TrimSuffix(baseFileName, w.baseFileExt)
	switch rollPeriod {
	case RollingPeriodYear, RollingPeriodMonth, RollingPeriodDay,
		RollingPeriodHour, RollingPeriodMinute, RollingPeriodSecond:
		w.rollPeriod = rollPeriod
	default:
		return nil, errors.New("unsupported roll period")
	}
	if err := w.tryRotate(); err != nil {
		return nil, err
	}
	return w, nil
}

// Close closes the file writer
func (w *TimeRollingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// Write writes data to the file
func (w *TimeRollingFileWriter) Write(bz []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.tryRotate(); err != nil {
		return 0, err
	}
	return w.file.Write(bz)
}

// tryRotate attempts to perform file rotation
func (w *TimeRollingFileWriter) tryRotate() error {
	var (
		fileName        string
		nextCheckTime   time.Time
		deleteCheckTime time.Time
		now             = time.Now()
	)

	if time.Now().Before(w.nextCheckTime) {
		return nil
	}

	if w.file != nil {
		_ = w.file.Close()
	}

	switch w.rollPeriod {
	case RollingPeriodYear:
		nextCheckTime = time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, now.Location())
		deleteCheckTime = time.Date(nextCheckTime.Year()-w.maxBackups, 1, 1, 0, 0, 0, 0, now.Location())
		fileName = fmt.Sprintf("%s.%d%s", w.baseFilePrefix, now.Year(), w.baseFileExt)

	case RollingPeriodMonth:
		nextCheckTime = time.Date(
			now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location(),
		).AddDate(0, 1, 0)
		deleteCheckTime = nextCheckTime.AddDate(0, -w.maxBackups, 0)
		fileName = fmt.Sprintf("%s.%s%s", w.baseFilePrefix, now.Format("200601"), w.baseFileExt)

	case RollingPeriodDay:
		nextCheckTime = time.Date(
			now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location(),
		).AddDate(0, 0, 1)
		deleteCheckTime = nextCheckTime.AddDate(0, 0, -w.maxBackups)
		fileName = fmt.Sprintf("%s.%s%s", w.baseFilePrefix, now.Format("20060102"), w.baseFileExt)

	case RollingPeriodHour:
		nextCheckTime = time.Date(
			now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location(),
		).Add(time.Hour)
		deleteCheckTime = nextCheckTime.Add(-time.Duration(w.maxBackups) * time.Hour)
		fileName = fmt.Sprintf("%s.%s%s", w.baseFilePrefix, now.Format("20060102_15"), w.baseFileExt)

	case RollingPeriodMinute:
		nextCheckTime = time.Date(
			now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, now.Location(),
		).Add(time.Minute)
		deleteCheckTime = nextCheckTime.Add(-time.Duration(w.maxBackups) * time.Minute)
		fileName = fmt.Sprintf("%s.%s%s", w.baseFilePrefix, now.Format("20060102_15_04"), w.baseFileExt)

	case RollingPeriodSecond:
		nextCheckTime = time.Date(
			now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, now.Location(),
		).Add(time.Second)
		deleteCheckTime = nextCheckTime.Add(-time.Duration(w.maxBackups) * time.Second)
		fileName = fmt.Sprintf("%s.%s%s", w.baseFilePrefix, now.Format("20060102_15_04_05"), w.baseFileExt)

	default:
		return errors.New("unsupported roll period")
	}

	// Open the new file
	file, err := os.OpenFile(filepath.Join(w.basePath, fileName), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	w.file = file

	// Set the next check time and delete check time
	w.nextCheckTime = nextCheckTime
	w.deleteCheckTime = deleteCheckTime

	// Try to delete old files
	go w.tryDeleteOldFiles()

	return nil
}

// tryDeleteOldFiles tries to delete old files based on the delete check time
func (w *TimeRollingFileWriter) tryDeleteOldFiles() {
	files, err := filepath.Glob(filepath.Join(w.basePath, "*"+w.baseFileExt))
	if err != nil {
		fmt.Println("error while globbing files:", err)
		return
	}
	if len(files) <= w.maxBackups {
		return
	}
	for _, file := range files {
		fileInfo, err := os.Stat(file)
		if err != nil {
			fmt.Println("error while getting file info:", err)
			continue
		}
		fileName := fileInfo.Name()
		fileName = strings.TrimSuffix(fileName, w.baseFileExt)
		fileDate := strings.TrimPrefix(fileName, w.baseFilePrefix+".")
		var fileTime time.Time
		switch w.rollPeriod {
		case RollingPeriodYear:
			fileTime, err = time.ParseInLocation("2006", fileDate, w.deleteCheckTime.Location())
		case RollingPeriodMonth:
			fileTime, err = time.ParseInLocation("200601", fileDate, w.deleteCheckTime.Location())
		case RollingPeriodDay:
			fileTime, err = time.ParseInLocation("20060102", fileDate, w.deleteCheckTime.Location())
		case RollingPeriodHour:
			fileTime, err = time.ParseInLocation("20060102_15", fileDate, w.deleteCheckTime.Location())
		case RollingPeriodMinute:
			fileTime, err = time.ParseInLocation("20060102_15_04", fileDate, w.deleteCheckTime.Location())
		case RollingPeriodSecond:
			fileTime, err = time.ParseInLocation("20060102_15_04_05", fileDate, w.deleteCheckTime.Location())
		default:
			panic("bug found! unexpected roll period value found")
		}
		if err != nil {
			fmt.Println("error while parsing file time")
			continue
		}
		// Check if the file is older than the delete check time
		if fileTime.Before(w.deleteCheckTime) {
			err = os.Remove(file)
			if err != nil {
				fmt.Println("failed to remove old file:", err)
			}
			return
		}
	}
}
