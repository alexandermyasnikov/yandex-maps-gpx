package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"

	"github.com/Marlliton/slogpretty"
	"gitlab.com/amyasnikov/yandex-maps-gpx/internal/converter"
)

var (
	apiKey    string
	publicIds string
	outputDir string
	cacheDir  string
	logLevel  slog.Level
)

func main() {
	flag.StringVar(&apiKey, "api-key", "", "API key for Yandex Maps API (or use environment variable YANDEX_GEOCODER_API_KEY)")
	flag.StringVar(&publicIds, "public-ids", "", "comma-separated list of public IDs to convert")
	flag.StringVar(&outputDir, "output-dir", "/tmp", "output file")
	flag.StringVar(&cacheDir, "cache-dir", "/tmp", "directory for persistent cache")
	flag.TextVar(&logLevel, "log-level", slog.LevelWarn, "log level")
	flag.Parse()

	if apiKey == "" {
		apiKey = os.Getenv("YANDEX_GEOCODER_API_KEY")
	}

	slog.SetDefault(slog.New(slogpretty.New(os.Stdout, &slogpretty.Options{
		Level:      logLevel,
		AddSource:  true,
		Colorful:   true,
		Multiline:  true,
		TimeFormat: slogpretty.DefaultTimeFormat,
	})))

	ctx := context.Background()

	for publicId := range strings.SplitSeq(publicIds, ",") {
		converter, err := converter.NewConverter(converter.Config{
			ApiKey:    apiKey,
			PublicId:  publicId,
			OutputDir: outputDir,
			CacheDir:  cacheDir,
		})
		if err != nil {
			slog.Error("failed to create converter", "error", err)
			return
		}

		if err := converter.Convert(ctx); err != nil {
			slog.Error("failed to convert", "error", err)
			return
		}
	}
}
