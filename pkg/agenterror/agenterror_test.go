package agenterror_test

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/pkg/agenterror"
)

var _ = Describe("AgentError", func() {
	Describe("the AgentError type", func() {
		It("wraps its cause and is unwrappable", func() {
			cause := errors.New("boom")
			e := agenterror.New(agenterror.CodeNeedsRoot, "checkRoot", "do the thing", cause)
			Expect(errors.Unwrap(e)).To(Equal(cause))
			Expect(errors.Is(e, cause)).To(BeTrue())
		})

		It("includes the op and the cause in its message", func() {
			e := agenterror.New(agenterror.CodeNeedsRoot, "checkRoot", "hint", errors.New("boom"))
			Expect(e.Error()).To(Equal("checkRoot: boom"))
		})

		It("omits the op prefix when op is empty", func() {
			e := agenterror.New(agenterror.CodeNeedsRoot, "", "hint", errors.New("boom"))
			Expect(e.Error()).To(Equal("boom"))
		})

		It("is discoverable through a wrapped chain via errors.As", func() {
			inner := agenterror.New(agenterror.CodeBadSource, "validateSource", "fix the source", errors.New("bad"))
			outer := fmt.Errorf("install failed: %w", inner)
			var ae *agenterror.AgentError
			Expect(errors.As(outer, &ae)).To(BeTrue())
			Expect(ae.Code).To(Equal(agenterror.CodeBadSource))
			Expect(ae.Hint).To(Equal("fix the source"))
		})
	})

	Describe("Classify", func() {
		It("prefers an explicit AgentError in the chain over substring rules", func() {
			// Cause text would also match the root-privileges rule, but the
			// typed error's own code/hint must win.
			inner := agenterror.New(agenterror.CodeBadSource, "op", "typed hint", errors.New("this command requires root privileges"))
			code, hint, matched := agenterror.Classify(fmt.Errorf("wrap: %w", inner))
			Expect(matched).To(BeTrue())
			Expect(code).To(Equal(agenterror.CodeBadSource))
			Expect(hint).To(Equal("typed hint"))
		})

		It("matches the root-privileges rule", func() {
			code, hint, matched := agenterror.Classify(errors.New("this command requires root privileges"))
			Expect(matched).To(BeTrue())
			Expect(code).To(Equal(agenterror.CodeNeedsRoot))
			Expect(hint).ToNot(BeEmpty())
		})

		It("matches the missing-target-device rule", func() {
			code, _, matched := agenterror.Classify(errors.New("no target device specified and no device found"))
			Expect(matched).To(BeTrue())
			Expect(code).To(Equal(agenterror.CodeTargetNotFound))
		})

		It("matches network failures regardless of case", func() {
			code, _, matched := agenterror.Classify(errors.New("dial tcp 1.2.3.4:443: Connection refused"))
			Expect(matched).To(BeTrue())
			Expect(code).To(Equal(agenterror.CodeNetwork))
		})

		It("matches the no-space rule", func() {
			code, _, matched := agenterror.Classify(errors.New("write /foo: no space left on device"))
			Expect(matched).To(BeTrue())
			Expect(code).To(Equal(agenterror.CodeNoSpace))
		})

		It("reports no match for an unknown error", func() {
			_, _, matched := agenterror.Classify(errors.New("some entirely novel failure"))
			Expect(matched).To(BeFalse())
		})

		It("reports no match for a nil error", func() {
			_, _, matched := agenterror.Classify(nil)
			Expect(matched).To(BeFalse())
		})
	})

	Describe("Format", func() {
		It("returns an empty string for a nil error", func() {
			Expect(agenterror.Format(nil)).To(BeEmpty())
		})

		It("returns an empty string for an empty-message sentinel error", func() {
			// main.go uses fmt.Errorf("") as a help-printed sentinel; it must
			// not produce a stray error block.
			Expect(agenterror.Format(errors.New(""))).To(BeEmpty())
		})

		It("prints just the error line for an unclassified error", func() {
			out := agenterror.Format(errors.New("mystery"))
			Expect(out).To(ContainSubstring("Error: mystery"))
			Expect(out).ToNot(ContainSubstring("Code:"))
			Expect(out).ToNot(ContainSubstring("Hint:"))
		})

		It("prints error, code and hint for a classified error", func() {
			out := agenterror.Format(errors.New("this command requires root privileges"))
			Expect(out).To(ContainSubstring("Error: this command requires root privileges"))
			Expect(out).To(ContainSubstring(fmt.Sprintf("Code:  %s", agenterror.CodeNeedsRoot)))
			Expect(out).To(ContainSubstring("Hint:"))
		})

		It("uses the typed error's hint when present", func() {
			e := agenterror.New(agenterror.CodeBadSource, "op", "prefix the source", errors.New("bad source"))
			out := agenterror.Format(e)
			Expect(out).To(ContainSubstring(fmt.Sprintf("Code:  %s", agenterror.CodeBadSource)))
			Expect(out).To(ContainSubstring("Hint:  prefix the source"))
		})
	})
})
