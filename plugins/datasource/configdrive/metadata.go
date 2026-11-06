package configdrive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/randomtoy/wininit/internal/plugin"
)

// Parse consumes only uuid and hostname. Unknown JSON fields are validated but
// never retained. Errors must not echo possibly sensitive metadata contents.
func Parse(data []byte) (plugin.Metadata, error) {
	if len(data) > MaxMetadataBytes {
		return plugin.Metadata{}, errors.New("ConfigDrive meta_data.json exceeds 64 KiB")
	}
	if !utf8.Valid(data) {
		return plugin.Metadata{}, errors.New("ConfigDrive metadata must be UTF-8 JSON")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return plugin.Metadata{}, errors.New("ConfigDrive metadata must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 0); err != nil {
		return plugin.Metadata{}, errors.New("invalid ConfigDrive JSON: duplicate keys, excessive nesting, or malformed data")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return plugin.Metadata{}, errors.New("ConfigDrive metadata must contain exactly one JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return plugin.Metadata{}, errors.New("invalid ConfigDrive JSON object")
	}
	id, err := stringField(fields["uuid"])
	if err != nil || id == "" || len(id) > 256 || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 || strings.ContainsRune(id, utf8.RuneError) {
		return plugin.Metadata{}, errors.New("ConfigDrive uuid must be a nonempty string of at most 256 bytes without surrounding whitespace or control characters")
	}
	metadata := plugin.Metadata{InstanceID: id}
	if raw, present := fields["hostname"]; present {
		hostname, err := stringField(raw)
		if err != nil {
			return plugin.Metadata{}, errors.New("ConfigDrive hostname must be a string")
		}
		metadata.LocalHostname, err = localHostname(hostname)
		if err != nil {
			return plugin.Metadata{}, err
		}
	}
	return metadata, nil
}

func stringField(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", errors.New("expected a JSON string")
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", err
	}
	return value, nil
}

// localHostname explicitly maps an OpenStack DNS hostname/FQDN to the first
// label. It never truncates a label to fit Windows; the provisioner validates
// the resulting label against its own Windows hostname policy.
func localHostname(hostname string) (string, error) {
	if hostname == "" {
		return "", nil
	}
	name := strings.TrimSuffix(hostname, ".")
	if name == "" || len(name) > 253 {
		return "", errors.New("ConfigDrive hostname must be a valid ASCII DNS hostname or FQDN")
	}
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("ConfigDrive hostname contains an invalid DNS label")
		}
		for index := range len(label) {
			ch := label[index]
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return "", errors.New("ConfigDrive hostname contains a non-DNS character")
			}
		}
	}
	return strings.ToLower(labels[0]), nil
}

func validateJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return errors.New("duplicate or invalid JSON key")
			}
			seen[key] = true
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
