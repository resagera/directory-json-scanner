package infrastructure

// Пакет ограничивает количество одновременных I/O операций
var ioSem chan struct{}

// InitIOLimiter инициализирует семафор
func InitIOLimiter(limit int) {
	ioSem = make(chan struct{}, limit)
}

// WithIOLimit выполняет функцию с ограничением одновременного доступа
func WithIOLimit(fn func()) {
	ioSem <- struct{}{}
	defer func() { <-ioSem }()
	fn()
}

// WithIOLimitValue обёртка для функций с возвратом значения
func WithIOLimitValue[T any](fn func() T) T {
	ioSem <- struct{}{}
	defer func() { <-ioSem }()
	return fn()
}
