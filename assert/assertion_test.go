package assert

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/zoncoen/query-go"
	"github.com/zoncoen/scenarigo/errors"
)

func TestBuild(t *testing.T) {
	str := `
deps:
- name: scenarigo
  version:
    major: 1
    minor: 2
    patch: 3
  tags:
    - go
    - test`
	var in interface{}
	if err := yaml.NewDecoder(strings.NewReader(str), yaml.UseOrderedMap()).Decode(&in); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	qs := []string{
		".deps[0].name",
		".deps[0].version.major",
		".deps[0].version.minor",
		".deps[0].version.patch",
		".deps[0].tags[0]",
		".deps[0].tags[1]",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	assertion, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	type info struct {
		Deps []map[string]interface{} `yaml:"deps"`
	}

	t.Run("no assertion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		assertion, err := Build(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		v := info{}
		if err := assertion.Assert(v); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("compare", func(t *testing.T) {
		if err := MustBuild(context.Background(), Greater(1)).Assert(2); err != nil {
			t.Fatal(err)
		}
		if err := MustBuild(context.Background(), GreaterOrEqual(1)).Assert(1); err != nil {
			t.Fatal(err)
		}
		if err := MustBuild(context.Background(), Less(2)).Assert(1); err != nil {
			t.Fatal(err)
		}
		if err := MustBuild(context.Background(), LessOrEqual(1)).Assert(1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ok", func(t *testing.T) {
		v := info{
			Deps: []map[string]interface{}{
				{
					"name": "scenarigo",
					"version": map[string]int{
						"major": 1,
						"minor": 2,
						"patch": 3,
					},
					"tags": []string{"go", "test"},
				},
			},
		}
		if err := assertion.Assert(v); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("ng", func(t *testing.T) {
		v := info{
			Deps: []map[string]interface{}{
				{
					"name": "Ruby on Rails",
					"version": map[string]int{
						"major": 2,
						"minor": 3,
						"patch": 4,
					},
					"tags": []string{"ruby", "http"},
				},
			},
		}
		err := assertion.Assert(v)
		if err == nil {
			t.Fatalf("expected error but no error")
		}
		var mperr *errors.MultiPathError
		if ok := errors.As(err, &mperr); !ok {
			t.Fatalf("expected errors.MultiPathError: %s", err)
		}
		if got, expect := len(mperr.Errs), len(qs); got != expect {
			t.Fatalf("expected %d but got %d", expect, got)
		}
		for i, e := range mperr.Errs {
			q := qs[i]
			if !strings.Contains(e.Error(), q) {
				t.Errorf(`"%s" does not contain "%s"`, e.Error(), q)
			}
		}
	})
	t.Run("assert nil", func(t *testing.T) {
		err := assertion.Assert(nil)
		if err == nil {
			t.Fatalf("expected error but no error")
		}
		var mperr *errors.MultiPathError
		if ok := errors.As(err, &mperr); !ok {
			t.Fatalf("expected errors.MultiPathError: %s", err)
		}
		if got, expect := len(mperr.Errs), len(qs); got != expect {
			t.Fatalf("expected %d but got %d", expect, got)
		}
		for i, e := range mperr.Errs {
			q := qs[i]
			if !strings.Contains(e.Error(), q) {
				t.Errorf(`"%s" does not contain "%s"`, e.Error(), q)
			}
		}
	})
	t.Run("options", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		assertion, err := Build(
			ctx, `{{aaa}}`,
			FromTemplate(map[string]string{"aaa": "foo"}),
			WithEqualers(EqualerFunc(func(a, b any) (bool, error) {
				return true, nil
			})),
		)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := assertion.Assert("bar"); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("use $", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		assertion, err := Build(ctx, `{{$ == "foo"}}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := assertion.Assert("foo"); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		// call Assert twice
		if err := assertion.Assert("bar"); err == nil {
			t.Error("no error")
		} else if got, expect := err.Error(), "assertion error"; got != expect {
			t.Errorf("expect %q but got %q", expect, got)
		}
	})
	t.Run("use $ twice", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		assertion, err := Build(ctx, `{{$ == $}}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := assertion.Assert("test"); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	t.Run("assertion result is not boolean", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		assertion, err := Build(ctx, `{{$ + $}}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := assertion.Assert(1); err == nil {
			t.Error("no error")
		} else if got, expect := err.Error(), "assertion result must be a boolean value but got int64"; got != expect {
			t.Errorf("expect %q but got %q", expect, got)
		}
	})
}

func TestWaitContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	wc := newWaitContext(ctx, map[string]string{"foo": "FOO"})
	if got, expect := extract(t, "$.foo", wc), "FOO"; got != expect {
		t.Fatalf("expect %q but got %q", expect, got)
	}

	// wait until setting a value
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if got, expect := extract(t, "$.$", wc), "BAR"; got != expect {
			t.Errorf("expect %q but got %q", expect, got)
		}
	}()
	go func() {
		defer wg.Done()
		if got, expect := extract(t, "$.$", wc), "BAR"; got != expect {
			t.Errorf("expect %q but got %q", expect, got)
		}
	}()

	if err := wc.set("BAR"); err != nil {
		t.Fatalf("failed to set: %s", err)
	}
	wg.Wait()

	// extract after setting
	if got, expect := extract(t, "$.$", wc), "BAR"; got != expect {
		t.Fatalf("expect %q but got %q", expect, got)
	}

	// don't set twice
	if err := wc.set("BAR"); err == nil {
		t.Fatal("no error")
	}
}

func extract(t *testing.T, s string, target any) any {
	t.Helper()
	q, err := query.ParseString(s)
	if err != nil {
		t.Fatalf("failed to parse query string: %s", err)
	}
	v, err := q.Extract(target)
	if err != nil {
		t.Fatalf("failed to extract: %s", err)
	}
	return v
}
