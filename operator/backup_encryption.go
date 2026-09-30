package operator

import (
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

func encryptBackupArchive(source, destination, recipientText string) error {
	recipientWriter, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	writer, err := newBackupEncryptWriter(recipientWriter, recipientText)
	if err != nil {
		_ = recipientWriter.Close()
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		_ = writer.Close()
		_ = recipientWriter.Close()
		return err
	}
	defer input.Close()
	if _, err := io.Copy(writer, input); err != nil {
		_ = writer.Close()
		_ = recipientWriter.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = recipientWriter.Close()
		return err
	}
	if err := recipientWriter.Sync(); err != nil {
		_ = recipientWriter.Close()
		return err
	}
	if err := recipientWriter.Close(); err != nil {
		return err
	}
	return os.Chmod(destination, 0o600)
}

func newBackupEncryptWriter(destination io.Writer, recipientText string) (io.WriteCloser, error) {
	recipient, err := age.ParseX25519Recipient(recipientText)
	if err != nil {
		return nil, fmt.Errorf("invalid age recipient")
	}
	return age.Encrypt(destination, recipient)
}
