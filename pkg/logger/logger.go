package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

var levelNames = []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// Logger 日志器
type Logger struct {
	mu         sync.Mutex
	level      Level
	output     io.Writer
	jsonFormat bool
	includeCtx bool
}

// 全局日志器
var defaultLogger = &Logger{
	level:      InfoLevel,
	output:     os.Stdout,
	jsonFormat: false,
	includeCtx: true,
}

// Config 日志配置
type Config struct {
	Level      string `yaml:"level"`       // 日志级别: debug, info, warn, error
	Path       string `yaml:"path"`        // 日志目录
	MaxSize    int    `yaml:"max_size"`    // 单文件最大 MB
	MaxBackups int    `yaml:"max_backups"` // 保留文件数
	MaxAge     int    `yaml:"max_age"`     // 保留天数
	Compress   bool   `yaml:"compress"`    // 压缩
	JSONFormat bool   `yaml:"json_format"` // JSON 格式
}

// Init 初始化日志
func Init(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{
			Level:      "info",
			Path:       "./logs",
			MaxSize:    50,
			MaxBackups: 30,
			MaxAge:     7,
			Compress:   true,
		}
	}

	// 设置日志级别
	switch strings.ToLower(cfg.Level) {
	case "debug":
		defaultLogger.level = DebugLevel
	case "info":
		defaultLogger.level = InfoLevel
	case "warn", "warning":
		defaultLogger.level = WarnLevel
	case "error":
		defaultLogger.level = ErrorLevel
	}

	// 设置 JSON 格式
	defaultLogger.jsonFormat = cfg.JSONFormat

	// 创建日志目录
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return err
	}

	// 创建文件写入器
	writer, err := NewFileWriter(cfg.Path, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge, cfg.Compress)
	if err != nil {
		return err
	}

	// 设置输出为文件
	defaultLogger.output = writer

	return nil
}

// SetLevel 设置日志级别
func SetLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		defaultLogger.level = DebugLevel
	case "info":
		defaultLogger.level = InfoLevel
	case "warn", "warning":
		defaultLogger.level = WarnLevel
	case "error":
		defaultLogger.level = ErrorLevel
	}
}

// SetOutput 设置输出
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.output = w
}

// Debug 调试日志
func Debug(args ...interface{}) {
	defaultLogger.logArgs(DebugLevel, args...)
}

// Debugf 格式化调试日志
func Debugf(format string, args ...interface{}) {
	defaultLogger.log(DebugLevel, format, args...)
}

// Info 信息日志
func Info(args ...interface{}) {
	defaultLogger.logArgs(InfoLevel, args...)
}

// Infof 格式化信息日志
func Infof(format string, args ...interface{}) {
	defaultLogger.log(InfoLevel, format, args...)
}

// Warn 警告日志
func Warn(args ...interface{}) {
	defaultLogger.logArgs(WarnLevel, args...)
}

// Warnf 格式化警告日志
func Warnf(format string, args ...interface{}) {
	defaultLogger.log(WarnLevel, format, args...)
}

// Error 错误日志
func Error(args ...interface{}) {
	defaultLogger.logArgs(ErrorLevel, args...)
}

// Errorf 格式化错误日志
func Errorf(format string, args ...interface{}) {
	defaultLogger.log(ErrorLevel, format, args...)
}

// Fatal 致命错误日志
func Fatal(args ...interface{}) {
	defaultLogger.logArgs(FatalLevel, args...)
	os.Exit(1)
}

// Fatalf 格式化致命错误日志
func Fatalf(format string, args ...interface{}) {
	defaultLogger.log(FatalLevel, format, args...)
	os.Exit(1)
}

// log 内部日志方法
func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 获取调用者信息
	pc, file, line, _ := runtime.Caller(2)
	fn := runtime.FuncForPC(pc)

	var msg string
	if format == "" {
		msg = fmt.Sprint(args...)
	} else {
		msg = fmt.Sprintf(format, args...)
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelName := levelNames[level]

	if l.jsonFormat {
		// JSON 格式输出
		_, _ = fmt.Fprintf(l.output, `{"time":"%s","level":"%s","file":"%s:%d","func":"%s","message":"%s"}`+"\n",
			timestamp, levelName, filepath.Base(file), line, fn.Name(), msg)
	} else {
		// 文本格式输出
		_, _ = fmt.Fprintf(l.output, "[%s] %s %s:%d (%s) %s\n",
			timestamp, levelName, filepath.Base(file), line, fn.Name(), msg)
	}
}

func (l *Logger) logArgs(level Level, args ...interface{}) {
	l.log(level, "%s", fmt.Sprint(args...))
}

// FileWriter 文件写入器（带轮转）
type FileWriter struct {
	mu         sync.Mutex
	filename   string
	file       *os.File
	maxSize    int
	maxBackups int
	maxAge     int
	compress   bool
}

// NewFileWriter 创建文件写入器
func NewFileWriter(dir string, maxSize, maxBackups, maxAge int, compress bool) (*FileWriter, error) {
	filename := filepath.Join(dir, "star-meet.log")

	// 创建或打开文件
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	w := &FileWriter{
		filename:   filename,
		file:       f,
		maxSize:    maxSize,
		maxBackups: maxBackups,
		maxAge:     maxAge,
		compress:   compress,
	}

	// 启动轮转检查
	go w.rotateCheck()

	return w, nil
}

// Write 写入
func (w *FileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否需要轮转
	w.maybeRotate()

	return w.file.Write(p)
}

// maybeRotate 检查是否需要轮转
func (w *FileWriter) maybeRotate() {
	info, err := w.file.Stat()
	if err != nil {
		return
	}

	// 如果文件大小超过限制，进行轮转
	if info.Size() > int64(w.maxSize*1024*1024) {
		w.rotate()
	}
}

// rotate 轮转日志
func (w *FileWriter) rotate() {
	// 关闭当前文件
	w.file.Close()

	// 轮转旧文件
	for i := w.maxBackups - 1; i >= 0; i-- {
		oldName := w.backupName(i)
		newName := w.backupName(i + 1)

		// 删除最旧的
		if i == w.maxBackups-1 {
			os.Remove(oldName)
			continue
		}

		// 重命名
		if _, err := os.Stat(oldName); err == nil {
			os.Rename(oldName, newName)
		}
	}

	// 创建新文件
	f, err := os.OpenFile(w.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("创建日志文件失败: %v", err)
		return
	}

	w.file = f
}

// backupName 获取备份文件名
func (w *FileWriter) backupName(num int) string {
	if num == 0 {
		return w.filename
	}
	return fmt.Sprintf("%s.%d", w.filename, num)
}

// rotateCheck 定期检查轮转
func (w *FileWriter) rotateCheck() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		w.mu.Lock()
		w.maybeRotate()
		w.mu.Unlock()
	}
}

// Close 关闭
func (w *FileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
