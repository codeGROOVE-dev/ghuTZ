package gemini

// CacheInterface defines the cache operations needed by the Gemini client
type CacheInterface interface {
	APICall(key string, requestPayload []byte) ([]byte, bool)
	SetAPICall(key string, requestPayload []byte, responseData []byte) error
}

// Logger defines the logging interface needed by the Gemini client
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
