package pgx

import (
	"errors"

	"github.com/jackc/pgx/v5/internal/pgio"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgreSQL format codes
const (
	TextFormatCode   = 0
	BinaryFormatCode = 1
)

func convertSimpleArgument(m *pgtype.Map, arg any) (any, error) {
	buf, err := m.Encode(0, TextFormatCode, arg, []byte{})
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, nil
	}
	return string(buf), nil
}

func encodeCopyValue(m *pgtype.Map, buf []byte, oid uint32, arg any) ([]byte, error) {
	sp := len(buf)
	buf = pgio.AppendInt32(buf, -1)
	argBuf, err := m.Encode(oid, BinaryFormatCode, arg, buf)
	if err != nil {
		if argBuf2, err2 := tryScanStringCopyValueThenEncode(m, buf, oid, arg); err2 == nil {
			argBuf = argBuf2
		} else {
			return nil, err
		}
	}

	if argBuf != nil {
		buf = argBuf
		pgio.SetInt32(buf[sp:], int32(len(buf[sp:])-4))
	}
	return buf, nil
}

func tryScanStringCopyValueThenEncode(m *pgtype.Map, buf []byte, oid uint32, arg any) ([]byte, error) {
	s, ok := arg.(string)
	if !ok {
		textBuf, err := m.Encode(oid, TextFormatCode, arg, nil)
		if err != nil {
			return nil, errors.New("not a string and cannot be encoded as text")
		}
		s = string(textBuf)
	}

	// Scan the text representation into a value that preserves type-specific
	// structure. By default, scanning into *any delegates to Codec.DecodeValue,
	// which is lossless for most types (e.g. text, numeric, bool).
	//
	// However, some codecs produce a lossy representation (e.g.
	// ArrayCodec.DecodeValue drops array dimensions). For those, scan into a
	// concrete type that retains the full structure needed for a faithful
	// binary re-encode.
	var v any
	switch codecForOID(m, oid).(type) {
	case *pgtype.ArrayCodec:
		// Scan into Array[any] because scanning into v directly goes through
		// ArrayCodec.DecodeValue which uses []any, a flat slice that discards
		// dimension information for multidimensional arrays.
		//
		// https://github.com/jackc/pgx/issues/2385
		var arr pgtype.Array[any]
		if err := m.Scan(oid, TextFormatCode, []byte(s), &arr); err != nil {
			return nil, err
		}
		v = arr
	default:
		if err := m.Scan(oid, TextFormatCode, []byte(s), &v); err != nil {
			return nil, err
		}
	}

	return m.Encode(oid, BinaryFormatCode, v, buf)
}

// codecForOID returns the Codec for the given OID, or nil if not found.
func codecForOID(m *pgtype.Map, oid uint32) pgtype.Codec {
	if t, ok := m.TypeForOID(oid); ok {
		return t.Codec
	}
	return nil
}
