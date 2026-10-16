package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/FlexEbat/Netscribe/internal/render"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// export writes the same file GET /api/export/{format} serves. It reads the local database
// directly, so the right to run it is the right to read the database file.
func export(args []string, e streams) error {
	flags := newCommonFlags("export", e.err)
	format := flags.fs.String("format", render.FormatMarkdown, "md, html, pdf or drawio")
	out := flags.fs.String("out", "", "file to write; empty or - writes to standard output")
	cfg, logger, err := flags.load(args, e.err)
	if err != nil {
		return err
	}
	if len(flags.args) > 0 {
		return fmt.Errorf("unexpected argument %q", flags.args[0])
	}
	switch *format {
	case render.FormatMarkdown, render.FormatHTML, render.FormatPDF, render.FormatDrawio:
	default:
		return fmt.Errorf("unknown format %q: use md, html, pdf or drawio", *format)
	}

	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	f, err := render.Export(context.Background(), st, *format, time.Now(), render.Options{ChromePath: cfg.Export.ChromePath})
	if err != nil {
		return err
	}
	if *out == "" || *out == "-" {
		_, err = e.out.Write(f.Body)
		return err
	}
	if err := os.WriteFile(*out, f.Body, 0o600); err != nil {
		return fmt.Errorf("write export: %w", err)
	}
	logger.Info("export written", "format", *format, "file", *out)
	return nil
}
