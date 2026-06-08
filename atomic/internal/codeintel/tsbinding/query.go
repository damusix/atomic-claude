package sitter

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type (
	Query struct {
		t TreeSitter
		q uint64
	}

	QueryCursor struct {
		t  TreeSitter
		qc uint64
	}

	QueryCapture struct {
		ID   uint32
		Node Node
	}

	QueryMatch struct {
		ID           uint32
		PatternIndex uint16
		Captures     []QueryCapture
	}
)

const (
	QueryErrorNone uint32 = iota
	QueryErrorSyntax
	QueryErrorNodeType
	QueryErrorField
	QueryErrorCapture
	QueryErrorStructure
	QueryErrorLanguage
)

func (t TreeSitter) NewQuery(ctx context.Context, pattern string, l Language) (Query, error) {
	errOffPtr, err := t.malloc.Call(ctx, 4)
	if err != nil {
		return Query{}, fmt.Errorf("allocating query error offset: %w", err)
	}
	defer t.free.Call(context.Background(), errOffPtr[0])
	errTypePtr, err := t.malloc.Call(ctx, 4)
	if err != nil {
		return Query{}, fmt.Errorf("allocating query error type: %w", err)
	}
	defer t.free.Call(context.Background(), errTypePtr[0])
	patternPtr, patternSize, freePattern, err := t.allocateString(ctx, pattern)
	if err != nil {
		return Query{}, fmt.Errorf("allocating pattern string: %w", err)
	}
	defer freePattern()
	queryPtr, err := t.queryNew.Call(ctx, l.l, patternPtr, patternSize, errOffPtr[0], errTypePtr[0])
	if err != nil {
		return Query{}, fmt.Errorf("creating query: %w", err)
	}
	errorOffset, ok := t.m.Memory().ReadUint32Le(uint32(errOffPtr[0]))
	if !ok {
		return Query{}, errors.New("invalid query error offset")
	}
	errorType, ok := t.m.Memory().ReadUint32Le(uint32(errTypePtr[0]))
	if !ok {
		return Query{}, errors.New("invalid query error type")
	}

	if errorType != QueryErrorNone {
		// search for the line containing the offset
		line := 1
		line_start := 0
		for i, c := range pattern {
			line_start = i
			if uint32(i) >= errorOffset {
				break
			}
			if c == '\n' {
				line++
			}
		}
		column := int(errorOffset) - line_start
		errorTypeToString := QueryErrorTypeToString(errorType)

		var message string
		switch errorType {
		// errors that apply to a single identifier
		case QueryErrorNodeType:
			fallthrough
		case QueryErrorField:
			fallthrough
		case QueryErrorCapture:
			// find identifier at input[errorOffset]
			// and report it in the error message
			s := string(pattern[errorOffset:])
			identifierRegexp := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*`)
			m := identifierRegexp.FindStringSubmatch(s)
			if len(m) > 0 {
				message = fmt.Sprintf("invalid %s '%s' at line %d column %d",
					errorTypeToString, m[0], line, column)
			} else {
				message = fmt.Sprintf("invalid %s at line %d column %d",
					errorTypeToString, line, column)
			}

		// errors the report position
		case QueryErrorSyntax:
			fallthrough
		case QueryErrorStructure:
			fallthrough
		case QueryErrorLanguage:
			fallthrough
		default:
			s := string(pattern[errorOffset:])
			lines := strings.Split(s, "\n")
			whitespace := strings.Repeat(" ", column)
			message = fmt.Sprintf("invalid %s at line %d column %d\n%s\n%s^",
				errorTypeToString, line, column,
				lines[0], whitespace)
		}

		return Query{}, errors.New(message)
	}

	return Query{t, queryPtr[0]}, nil
}

func (q Query) CaptureNameForID(ctx context.Context, id uint32) (string, error) {
	strlenPtr, err := q.t.malloc.Call(ctx, 4)
	if err != nil {
		return "", fmt.Errorf("allocating string length: %w", err)
	}
	defer q.t.free.Call(context.Background(), strlenPtr[0])
	namePtr, err := q.t.queryCaptureNameForID.Call(ctx, q.q, uint64(id), strlenPtr[0])
	if err != nil {
		return "", fmt.Errorf("getting capture name for id: %w", err)
	}
	strlen, ok := q.t.m.Memory().ReadUint32Le(uint32(strlenPtr[0]))
	if !ok {
		return "", errors.New("invalid str len")
	}
	captureName, ok := q.t.m.Memory().Read(uint32(namePtr[0]), strlen)
	if !ok {
		return "", errors.New("invalid capture name")
	}
	return string(captureName), nil
}

func (t TreeSitter) NewQueryCursor(ctx context.Context) (QueryCursor, error) {
	qc, err := t.queryCursorNew.Call(ctx)
	if err != nil {
		return QueryCursor{}, fmt.Errorf("creating query cursor: %w", err)
	}
	return QueryCursor{t, qc[0]}, nil
}

func (qc QueryCursor) Exec(ctx context.Context, q Query, n Node) error {
	_, err := qc.t.queryCusorExec.Call(ctx, qc.qc, q.q, n.n)
	return err
}

func (t TreeSitter) allocateQueryMatch(ctx context.Context) (uint64, error) {
	// allocate tsquerymatch 12 bytes
	nodePtr, err := t.malloc.Call(ctx, uint64(12))
	if err != nil {
		return 0, fmt.Errorf("allocating query match: %w", err)
	}
	return nodePtr[0], nil
}

func (qc QueryCursor) NextMatch(ctx context.Context) (QueryMatch, bool, error) {
	queryMatchPtr, err := qc.t.allocateQueryMatch(ctx)
	if err != nil {
		return QueryMatch{}, false, err
	}
	hasNextMatch, err := qc.t.queryCursorNextMatch.Call(ctx, qc.qc, queryMatchPtr)
	if err != nil {
		return QueryMatch{}, false, fmt.Errorf("getting query cursor next match: %w", err)
	}
	if hasNextMatch[0] == 0 {
		return QueryMatch{}, false, nil
	}

	queryMatchID, ok := qc.t.m.Memory().ReadUint32Le(uint32(queryMatchPtr))
	if !ok {
		return QueryMatch{}, false, errors.New("invalid query match id")
	}
	queryMatchPatternIndex, ok := qc.t.m.Memory().ReadUint16Le(uint32(queryMatchPtr) + 4)
	if !ok {
		return QueryMatch{}, false, errors.New("invalid query match pattern index")
	}
	queryMatchCaptureCount, ok := qc.t.m.Memory().ReadUint16Le(uint32(queryMatchPtr) + 6)
	if !ok {
		return QueryMatch{}, false, errors.New("invalid query match pattern index")
	}
	queryMatchCapturesPtr, ok := qc.t.m.Memory().ReadUint32Le(uint32(queryMatchPtr) + 8)
	if !ok {
		return QueryMatch{}, false, errors.New("invalid query match captures pointer")
	}
	qcs := make([]QueryCapture, queryMatchCaptureCount)
	addr := queryMatchCapturesPtr
	for i := range queryMatchCaptureCount {
		captureIndex, ok := qc.t.m.Memory().ReadUint32Le(addr + 24)
		if !ok {
			return QueryMatch{}, false, errors.New("invalid capture index")
		}
		qcs[i] = QueryCapture{
			ID:   captureIndex,
			Node: newNode(qc.t, uint64(addr)),
		}
		addr += 28
	}
	return QueryMatch{
		ID:           queryMatchID,
		PatternIndex: queryMatchPatternIndex,
		Captures:     qcs,
	}, true, nil
}

func QueryErrorTypeToString(errorType uint32) string {
	switch errorType {
	case QueryErrorNone:
		return "none"
	case QueryErrorNodeType:
		return "node type"
	case QueryErrorField:
		return "field"
	case QueryErrorCapture:
		return "capture"
	case QueryErrorSyntax:
		return "syntax"
	default:
		return "unknown"
	}
}
