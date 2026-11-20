package hooks

import (
	"fmt"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/tailscale/hujson"
)

const (
	realmSessionEvent   = "SessionStart"
	realmSessionMatcher = "startup"
	realmSessionCommand = sessionStartCommand + " --realm-instructions"
)

// ProjectRealmSessionStart adds or removes a witnessed local Claude hook member.
func ProjectRealmSessionStart(settings []byte, ownedDigest string, enable bool) ([]byte, string, error) {
	if !enable && ownedDigest == "" {
		return settings, "", nil
	}
	ast, arr, index, digest, err := observeRealmSessionStart(settings)
	if err != nil {
		return settings, "", err
	}
	if ownedDigest == "" && index != -1 {
		return settings, "", fmt.Errorf("Claude local SessionStart already contains an unowned realm hook command")
	}
	if digest != ownedDigest {
		return settings, "", fmt.Errorf("Claude local SessionStart realm hook does not match its ownership receipt; preserve it and resolve the conflict before retrying")
	}
	if enable {
		if index != -1 {
			return settings, digest, nil
		}
		if err := astRegisterEvent(&ast, realmSessionEvent, realmSessionMatcher, realmSessionCommand); err != nil {
			return settings, "", err
		}
		arr = eventArray(&ast, realmSessionEvent)
		return ast.Pack(), managedfile.Digest(arr.Elements[len(arr.Elements)-1].Pack()), nil
	}

	entry := arr.Elements[index]
	if index+1 < len(arr.Elements) {
		next := &arr.Elements[index+1]
		next.BeforeExtra = slices.Concat(entry.BeforeExtra, entry.AfterExtra, next.BeforeExtra)
	} else {
		arr.AfterExtra = slices.Concat(entry.BeforeExtra, entry.AfterExtra, arr.AfterExtra)
	}
	arr.Elements = slices.Delete(arr.Elements, index, index+1)
	return ast.Pack(), "", nil
}

// RealmSessionStartOwned reports whether exactly one local member matches the receipt.
func RealmSessionStartOwned(settings []byte, ownedDigest string) bool {
	if ownedDigest == "" {
		return false
	}
	_, _, _, digest, err := observeRealmSessionStart(settings)
	return err == nil && digest == ownedDigest
}

func observeRealmSessionStart(settings []byte) (hujson.Value, *hujson.Array, int, string, error) {
	if len(settings) == 0 {
		return hujson.Value{}, nil, -1, "", nil
	}
	ast, err := hujson.Parse(settings)
	if err != nil {
		return ast, nil, -1, "", fmt.Errorf("parse Claude local settings: %w", err)
	}
	arr, err := localSessionStartArray(ast)
	if err != nil || arr == nil {
		return ast, nil, -1, "", err
	}
	index := -1
	for i, entry := range arr.Elements {
		obj, ok := entry.Value.(*hujson.Object)
		if !ok {
			continue
		}
		handlers, err := uniqueLocalHookKey(obj, "hooks")
		if err != nil {
			return ast, nil, -1, "", err
		}
		if handlers == nil {
			continue
		}
		inner, ok := handlers.Value.(*hujson.Array)
		if !ok {
			continue
		}
		for _, handler := range inner.Elements {
			obj, ok := handler.Value.(*hujson.Object)
			if !ok {
				continue
			}
			command, err := uniqueLocalHookKey(obj, "command")
			if err != nil {
				return ast, nil, -1, "", err
			}
			if command == nil {
				continue
			}
			if literal, ok := command.Value.(hujson.Literal); !ok || literal.String() != realmSessionCommand {
				continue
			}
			if index != -1 {
				return ast, nil, -1, "", fmt.Errorf("Claude local SessionStart contains multiple realm hook commands")
			}
			index = i
		}
	}
	if index == -1 {
		return ast, arr, -1, "", nil
	}
	entry := arr.Elements[index]
	return ast, arr, index, managedfile.Digest(settings[entry.StartOffset:entry.EndOffset]), nil
}

func localSessionStartArray(ast hujson.Value) (*hujson.Array, error) {
	top, ok := ast.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("Claude local settings must be an object")
	}
	hooks, err := uniqueLocalHookKey(top, "hooks")
	if err != nil || hooks == nil {
		return nil, err
	}
	obj, ok := hooks.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf(`Claude local settings "hooks" must be an object`)
	}
	event, err := uniqueLocalHookKey(obj, realmSessionEvent)
	if err != nil || event == nil {
		return nil, err
	}
	arr, ok := event.Value.(*hujson.Array)
	if !ok {
		return nil, fmt.Errorf(`Claude local settings "hooks"."SessionStart" must be an array`)
	}
	return arr, nil
}

func uniqueLocalHookKey(obj *hujson.Object, key string) (*hujson.Value, error) {
	var result *hujson.Value
	for i := range obj.Members {
		member := &obj.Members[i]
		if member.Name.Value.(hujson.Literal).String() != key {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("Claude local hook settings contain duplicate %q keys", key)
		}
		result = &member.Value
	}
	return result, nil
}
