package app

// ScanConfig — параметры сканирования
type ScanConfig struct {
	RootDir string
	Exclude []string
	Output  string
	Pretty  bool
	Workers int
	SkipMD5 bool
	IOLimit int
	Resume  bool // TODO: пока не реализовано в stream-режиме
}

// MergeConfig — параметры объединения
type MergeConfig struct {
	Files         []string
	Output        string
	Pretty        bool
	Dedupe        bool
	MergeFlat     bool
	MergeChildren bool
}
