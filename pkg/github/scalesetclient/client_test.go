package scalesetclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestScaleSetClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ScaleSet Client Suite")
}

const maxRunners = 5

func newClientAgainst(s *stub) *Client {
	client, err := newSDKClientForURL(s.configURL())
	Expect(err).NotTo(HaveOccurred())
	return client
}

func newSDKClientForURL(configURL string) (*Client, error) {
	return New(&env.Data{
		GitHubURL:               configURL,
		GitHubAppID:             123,
		GitHubAppInstallationID: 456,
		GitHubAppPrivateKey:     mustAppPrivateKey(),
	}, maxRunners)
}

var _ = BeforeSuite(func() {
	logging.SetupLogger("error")
})

var _ = Describe("SDK-backed client against a GHES-shaped stub", func() {
	var (
		s      *stub
		ctx    context.Context
		client *Client
	)

	BeforeEach(func() {
		s = newStub()
		ctx = context.Background()
	})

	AfterEach(func() {
		s.close()
	})

	Describe("startup with jobs already in available", func() {
		BeforeEach(func() {
			s.setStatistics(map[string]int{"totalAvailableJobs": 2, "totalAssignedJobs": 0})
			client = newClientAgainst(s)
		})

		It("reports no acquirable jobs, because the SDK dropped that endpoint", func() {
			session, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())
			Expect(session.Statistics.TotalAvailableJobs).To(Equal(2))

			jobs, err := client.GetAcquirableJobs(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(jobs.Jobs).To(BeEmpty(), "the acquirablejobs endpoint is never called")

			_, _, acquired := s.observed()
			Expect(acquired).To(BeEmpty())
		})

		It("recovers the pre-queued jobs when the broker delivers them", func() {
			s.messageEnvelope = jobMessagesEnvelope(1, s.statisticsBody(), []map[string]any{
				jobAvailable(1001), jobAvailable(1002),
			})

			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())

			msg, err := client.GetMessage(ctx, "", "", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())
			Expect(msg.MessageType).To(Equal(jobMessagesType))
			Expect(msg.Body).To(ContainSubstring("1001"))
			Expect(msg.Body).To(ContainSubstring("1002"))

			_, err = client.AcquireJobs(ctx, 1, "", []int64{1001, 1002})
			Expect(err).NotTo(HaveOccurred())

			_, _, acquired := s.observed()
			Expect(acquired).To(Equal([][]int64{{1001, 1002}}))
		})

		It("strands the jobs when the broker stays silent", func() {
			s.messageQueueEmpty = true

			session, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())
			Expect(session.Statistics.TotalAvailableJobs).To(Equal(2))

			for range 3 {
				msg, err := client.GetMessage(ctx, "", "", 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(msg).To(BeNil(), "202 Accepted means no message")
			}

			calls, _, acquired := s.observed()
			Expect(calls).To(Equal(3))
			Expect(acquired).To(BeEmpty(), "nothing was ever acquired despite 2 available jobs")
		})
	})

	Describe("unrecognised outer message type", func() {
		BeforeEach(func() {
			client = newClientAgainst(s)
		})

		It("returns an error instead of skipping the message", func() {
			s.messageEnvelope = typedEnvelope(1, "SomeFutureMessageType", s.statisticsBody(), nil)

			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())

			_, err = client.GetMessage(ctx, "", "", 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported message type"))
		})

		It("tolerates unrecognised inner job message types", func() {
			s.messageEnvelope = jobMessagesEnvelope(1, s.statisticsBody(), []map[string]any{
				{"messageType": "SomeFutureJobMessage", "runnerRequestId": 99},
			})

			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())

			msg, err := client.GetMessage(ctx, "", "", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())
		})
	})

	Describe("X-ScaleSetMaxCapacity header", func() {
		BeforeEach(func() {
			client = newClientAgainst(s)
		})

		It("is sent on every GetMessage", func() {
			s.messageQueueEmpty = true

			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())

			_, err = client.GetMessage(ctx, "", "", 0)
			Expect(err).NotTo(HaveOccurred())

			_, headers, _ := s.observed()
			Expect(headers).To(ConsistOf("5"))
		})

		It("surfaces an error if the server rejects the header", func() {
			s.messageStatus = http.StatusBadRequest

			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).NotTo(HaveOccurred())

			_, err = client.GetMessage(ctx, "", "", 0)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("409 session conflict", func() {
		BeforeEach(func() {
			s.sessionStatus = http.StatusConflict
			client = newClientAgainst(s)
		})

		It("surfaces the conflict only as text, not as an inspectable status", func() {
			_, err := client.CreateMessageSession(ctx, 1, "owner")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("409"))
		})
	})

	Describe("scale set and runner operations", func() {
		BeforeEach(func() {
			client = newClientAgainst(s)
		})

		It("round-trips a scale set lookup", func() {
			scaleSet, err := client.GetRunnerScaleSet(ctx, 1, "orka-runners")
			Expect(err).NotTo(HaveOccurred())
			Expect(scaleSet).NotTo(BeNil())
			Expect(scaleSet.Id).To(Equal(1))
			Expect(scaleSet.Name).To(Equal("orka-runners"))
			Expect(scaleSet.RunnerSetting.DisableUpdate).To(BeTrue())
			Expect(scaleSet.Labels).To(HaveLen(1))
		})

		It("generates a JIT runner config", func() {
			cfg, err := client.CreateRunner(ctx, 1, "runner-7")
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.EncodedJITConfig).To(Equal("encoded-jit-config"))
			Expect(cfg.Runner.Id).To(Equal(7))
		})
	})
})
