package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"nuan/internal/store"
)

func migrateDB(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("migrate-db", flag.ContinueOnError)
	flags.SetOutput(out)
	from := flags.String("from", "", "source SQLite path or Postgres URL")
	to := flags.String("to", "", "destination SQLite path or Postgres URL")
	force := flags.Bool("force", false, "clear destination business tables before copying")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" || flags.NArg() != 0 {
		return errors.New("usage: nuan-server migrate-db --from <dsn> --to <dsn> [--force]")
	}
	if *from == *to {
		return errors.New("source and destination must be different databases")
	}
	source, err := store.Open(*from)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()
	target, err := store.Open(*to)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	defer target.Close()
	counts, err := source.CopyTo(context.Background(), target, *force)
	if err != nil {
		return err
	}
	for _, c := range counts {
		if _, err := fmt.Fprintf(out, "%s: source=%d destination=%d\n", c.Table, c.Source, c.Destination); err != nil {
			return err
		}
	}
	return nil
}
