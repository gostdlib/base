package hashmap

import (
	"fmt"
	"testing"
	"time"
)

func TestTTLExpiration(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: entry with TTL in future should be retrievable",
			wantErr: false,
		},
		{
			name:    "Success: entry with expired TTL should not be retrievable",
			wantErr: false,
		},
		{
			name:    "Success: entry with zero TTL should never expire",
			wantErr: false,
		},
		{
			name:    "Success: expired entry should be deleted on Get",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: entry with TTL in future should be retrievable":
					m := New[string, int](10)
					future := time.Now().Add(1 * time.Hour)
					m.Set("key1", 100, future)

					val, ok := m.Get("key1")
					if !ok {
						return fmt.Errorf("TestTTLExpiration(%s): expected to retrieve non-expired entry", test.name)
					}
					if val != 100 {
						return fmt.Errorf("TestTTLExpiration(%s): expected value 100, got %d", test.name, val)
					}
					return nil

				case "Success: entry with expired TTL should not be retrievable":
					m := New[string, int](10)
					past := time.Now().Add(-1 * time.Hour)
					m.Set("key1", 100, past)

					val, ok := m.Get("key1")
					if ok {
						return fmt.Errorf("TestTTLExpiration(%s): expected expired entry to not be retrievable, got value %d", test.name, val)
					}
					if m.Len() != 0 {
						return fmt.Errorf("TestTTLExpiration(%s): expected expired entry to be removed from map, length is %d", test.name, m.Len())
					}
					return nil

				case "Success: entry with zero TTL should never expire":
					m := New[string, int](10)
					m.Set("key1", 100, time.Time{})

					time.Sleep(10 * time.Millisecond)
					val, ok := m.Get("key1")
					if !ok {
						return fmt.Errorf("TestTTLExpiration(%s): expected entry with zero TTL to never expire", test.name)
					}
					if val != 100 {
						return fmt.Errorf("TestTTLExpiration(%s): expected value 100, got %d", test.name, val)
					}
					return nil

				case "Success: expired entry should be deleted on Get":
					m := New[string, int](10)
					past := time.Now().Add(-1 * time.Second)
					m.Set("key1", 100, past)

					if m.Len() != 1 {
						return fmt.Errorf("TestTTLExpiration(%s): expected map to have 1 entry before Get, got %d", test.name, m.Len())
					}

					m.Get("key1")

					if m.Len() != 0 {
						return fmt.Errorf("TestTTLExpiration(%s): expected expired entry to be deleted after Get, length is %d", test.name, m.Len())
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestTTLExpiration(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestTTLExpiration(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}

func TestTTLBoundary(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: entry expiring right at current time should be considered expired",
			wantErr: false,
		},
		{
			name:    "Success: entry expiring 1ms in future should not be expired",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: entry expiring right at current time should be considered expired":
					m := New[string, int](10)
					now := time.Now()
					m.Set("key1", 100, now)

					time.Sleep(1 * time.Millisecond)
					_, ok := m.Get("key1")
					if ok {
						return fmt.Errorf("TestTTLBoundary(%s): expected entry to be expired", test.name)
					}
					return nil

				case "Success: entry expiring 1ms in future should not be expired":
					m := New[string, int](10)
					future := time.Now().Add(100 * time.Millisecond)
					m.Set("key1", 100, future)

					val, ok := m.Get("key1")
					if !ok {
						return fmt.Errorf("TestTTLBoundary(%s): expected entry to not be expired", test.name)
					}
					if val != 100 {
						return fmt.Errorf("TestTTLBoundary(%s): expected value 100, got %d", test.name, val)
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestTTLBoundary(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestTTLBoundary(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}

func TestDeleteIfMaxTTL(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: delete entry when maxTTL matches",
			wantErr: false,
		},
		{
			name:    "Success: do not delete entry when maxTTL does not match",
			wantErr: false,
		},
		{
			name:    "Success: return false for non-existent key",
			wantErr: false,
		},
		{
			name:    "Success: delete entry with zero maxTTL when both are zero",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: delete entry when maxTTL matches":
					m := New[string, int](10)
					ttl := time.Now().Add(1 * time.Hour)
					m.Set("key1", 100, ttl)

					val, deleted := m.DeleteIfMaxTTL("key1", ttl)
					if !deleted {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected entry to be deleted when maxTTL matches", test.name)
					}
					if val != 100 {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected value 100, got %d", test.name, val)
					}
					if m.Len() != 0 {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected map to be empty after delete", test.name)
					}
					return nil

				case "Success: do not delete entry when maxTTL does not match":
					m := New[string, int](10)
					ttl1 := time.Now().Add(1 * time.Hour)
					ttl2 := time.Now().Add(2 * time.Hour)
					m.Set("key1", 100, ttl1)

					_, deleted := m.DeleteIfMaxTTL("key1", ttl2)
					if deleted {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected entry to not be deleted when maxTTL does not match", test.name)
					}
					if m.Len() != 1 {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected map to still have 1 entry", test.name)
					}

					val, ok := m.Get("key1")
					if !ok || val != 100 {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected original entry to still exist", test.name)
					}
					return nil

				case "Success: return false for non-existent key":
					m := New[string, int](10)
					ttl := time.Now().Add(1 * time.Hour)

					_, deleted := m.DeleteIfMaxTTL("key1", ttl)
					if deleted {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected false for non-existent key", test.name)
					}
					return nil

				case "Success: delete entry with zero maxTTL when both are zero":
					m := New[string, int](10)
					m.Set("key1", 100, time.Time{})

					val, deleted := m.DeleteIfMaxTTL("key1", time.Time{})
					if !deleted {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected entry to be deleted when both maxTTL are zero", test.name)
					}
					if val != 100 {
						return fmt.Errorf("TestDeleteIfMaxTTL(%s): expected value 100, got %d", test.name, val)
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestDeleteIfMaxTTL(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestDeleteIfMaxTTL(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}

func TestTTLWithResize(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: TTL values preserved during map resize",
			wantErr: false,
		},
		{
			name:    "Success: expired entries remain expired after resize",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: TTL values preserved during map resize":
					m := New[string, int](2)
					future := time.Now().Add(1 * time.Hour)

					m.Set("key1", 1, future)
					m.Set("key2", 2, future)
					m.Set("key3", 3, future)
					m.Set("key4", 4, future)
					m.Set("key5", 5, future)

					for i := 1; i <= 5; i++ {
						key := fmt.Sprintf("key%d", i)
						val, ok := m.Get(key)
						if !ok {
							return fmt.Errorf("TestTTLWithResize(%s): expected to retrieve %s after resize", test.name, key)
						}
						if val != i {
							return fmt.Errorf("TestTTLWithResize(%s): expected value %d for %s, got %d", test.name, i, key, val)
						}
					}
					return nil

				case "Success: expired entries remain expired after resize":
					m := New[string, int](2)
					past := time.Now().Add(-1 * time.Hour)

					m.Set("key1", 1, past)
					m.Set("key2", 2, past)
					m.Set("key3", 3, past)
					m.Set("key4", 4, past)
					m.Set("key5", 5, past)

					for i := 1; i <= 5; i++ {
						key := fmt.Sprintf("key%d", i)
						_, ok := m.Get(key)
						if ok {
							return fmt.Errorf("TestTTLWithResize(%s): expected expired entry %s to not be retrievable after resize", test.name, key)
						}
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestTTLWithResize(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestTTLWithResize(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}

func TestTTLWithCopy(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: copied map preserves TTL values",
			wantErr: false,
		},
		{
			name:    "Success: copied map has independent TTL expiration",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: copied map preserves TTL values":
					m1 := New[string, int](10)
					future := time.Now().Add(1 * time.Hour)
					m1.Set("key1", 100, future)
					m1.Set("key2", 200, future)

					m2 := m1.Copy()

					val, ok := m2.Get("key1")
					if !ok || val != 100 {
						return fmt.Errorf("TestTTLWithCopy(%s): expected copied map to have key1", test.name)
					}
					val, ok = m2.Get("key2")
					if !ok || val != 200 {
						return fmt.Errorf("TestTTLWithCopy(%s): expected copied map to have key2", test.name)
					}
					return nil

				case "Success: copied map has independent TTL expiration":
					m1 := New[string, int](10)
					past := time.Now().Add(-1 * time.Hour)
					m1.Set("key1", 100, past)

					m2 := m1.Copy()

					_, ok := m2.Get("key1")
					if ok {
						return fmt.Errorf("TestTTLWithCopy(%s): expected expired entry in copied map to not be retrievable", test.name)
					}

					if m1.Len() != 1 {
						return fmt.Errorf("TestTTLWithCopy(%s): expected original map to still have expired entry until accessed", test.name)
					}
					if m2.Len() != 0 {
						return fmt.Errorf("TestTTLWithCopy(%s): expected copied map to have removed expired entry", test.name)
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestTTLWithCopy(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestTTLWithCopy(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}

func TestTTLUpdateSameKey(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "Success: updating key with new TTL replaces old TTL",
			wantErr: false,
		},
		{
			name:    "Success: updating expired key with future TTL makes it retrievable",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := func() error {
				switch test.name {
				case "Success: updating key with new TTL replaces old TTL":
					m := New[string, int](10)
					ttl1 := time.Now().Add(1 * time.Hour)
					ttl2 := time.Now().Add(2 * time.Hour)

					m.Set("key1", 100, ttl1)
					prev, replaced := m.Set("key1", 200, ttl2)

					if !replaced {
						return fmt.Errorf("TestTTLUpdateSameKey(%s): expected Set to return true for replaced key", test.name)
					}
					if prev != 100 {
						return fmt.Errorf("TestTTLUpdateSameKey(%s): expected previous value 100, got %d", test.name, prev)
					}

					val, ok := m.Get("key1")
					if !ok || val != 200 {
						return fmt.Errorf("TestTTLUpdateSameKey(%s): expected updated value 200", test.name)
					}
					return nil

				case "Success: updating expired key with future TTL makes it retrievable":
					m := New[string, int](10)
					past := time.Now().Add(-1 * time.Hour)
					future := time.Now().Add(1 * time.Hour)

					m.Set("key1", 100, past)
					m.Set("key1", 200, future)

					val, ok := m.Get("key1")
					if !ok {
						return fmt.Errorf("TestTTLUpdateSameKey(%s): expected key to be retrievable after update with future TTL", test.name)
					}
					if val != 200 {
						return fmt.Errorf("TestTTLUpdateSameKey(%s): expected value 200, got %d", test.name, val)
					}
					return nil
				}
				return nil
			}()
			switch {
			case err == nil && test.wantErr:
				t.Errorf("TestTTLUpdateSameKey(%s): got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("TestTTLUpdateSameKey(%s): got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}
		})
	}
}
