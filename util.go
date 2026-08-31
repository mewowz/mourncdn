package main

import (
	"fmt"
	"io"
	"os"
)

func checkDirRW(dir string) error {
	f, err := os.CreateTemp(dir, ".permcheck-*")
	if err != nil {
		return fmt.Errorf("cannot create file in %q: %w", dir, err)
	}

	fName := f.Name()
	defer os.Remove(fName)

	if _, err := f.Write([]byte{0x0}); err != nil {
		f.Close()
		return fmt.Errorf("cannot write in %q: %w", dir, err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return fmt.Errorf("cannot seek temp file %q: %w", fName, err)
	}

	buf := make([]byte, 1)
	if _, err := io.ReadFull(f, buf); err != nil {
		f.Close()
		return fmt.Errorf("cannot read in %q: %w", dir, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("cannot close temp file %q: %w", fName, err)
	}

	return nil
}
