package trc

import (
	"testing"
)

func BenchmarkNewCoreEvent(b *testing.B) {
	b.ReportAllocs()

	b.Run("with callstacks", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			f16(flagLazy)
		}
	})

	b.Run("no callstacks", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			f16(flagLazy | flagNoStack)
		}
	})
}

func f0(flags uint8)  { _ = newCoreEvent(flags, "static string") }
func f1(flags uint8)  { f0(flags) }
func f2(flags uint8)  { f1(flags) }
func f3(flags uint8)  { f2(flags) }
func f4(flags uint8)  { f3(flags) }
func f5(flags uint8)  { f4(flags) }
func f6(flags uint8)  { f5(flags) }
func f7(flags uint8)  { f6(flags) }
func f8(flags uint8)  { f7(flags) }
func f9(flags uint8)  { f8(flags) }
func f10(flags uint8) { f9(flags) }
func f11(flags uint8) { f10(flags) }
func f12(flags uint8) { f11(flags) }
func f13(flags uint8) { f12(flags) }
func f14(flags uint8) { f13(flags) }
func f15(flags uint8) { f14(flags) }
func f16(flags uint8) { f15(flags) }
