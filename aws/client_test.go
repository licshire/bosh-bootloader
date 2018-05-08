package aws_test

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/cloudfoundry/bosh-bootloader/aws"
	"github.com/cloudfoundry/bosh-bootloader/fakes"
	"github.com/cloudfoundry/bosh-bootloader/storage"

	awslib "github.com/aws/aws-sdk-go/aws"
	awsec2 "github.com/aws/aws-sdk-go/service/ec2"
	awsroute53 "github.com/aws/aws-sdk-go/service/route53"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	Describe("NewClient", func() {
		It("returns a Client with the provided configuration", func() {
			client := aws.NewClient(
				storage.AWS{
					AccessKeyID:     "some-access-key-id",
					SecretAccessKey: "some-secret-access-key",
					Region:          "some-region",
				},
				&fakes.Logger{},
			)

			ec2Client, ok := client.GetEC2Client().(*awsec2.EC2)
			Expect(ok).To(BeTrue())

			_, ok = client.GetRoute53Client().(*awsroute53.Route53)
			Expect(ok).To(BeTrue())

			Expect(ec2Client.Config.Credentials).To(Equal(credentials.NewStaticCredentials("some-access-key-id", "some-secret-access-key", "")))
			Expect(ec2Client.Config.Region).To(Equal(awslib.String("some-region")))
		})
	})

	Describe("RetrieveDNS", func() {
		var (
			client        aws.Client
			route53Client *fakes.AWSRoute53Client
		)

		BeforeEach(func() {
			route53Client = &fakes.AWSRoute53Client{}
			client = aws.NewClientWithInjectedRoute53Client(route53Client, &fakes.Logger{})

			route53Client.ListHostedZonesByNameCall.Returns.Output = &awsroute53.ListHostedZonesByNameOutput{
				HostedZones: []*awsroute53.HostedZone{{
					Name: awslib.String("the-domain"),
					Id:   awslib.String("the-id"),
				}},
			}
			route53Client.GetHostedZoneCall.Returns.Output = &awsroute53.GetHostedZoneOutput{
				DelegationSet: &awsroute53.DelegationSet{
					NameServers: []*string{awslib.String("ns1"), awslib.String("ns2")},
				}}
		})

		It("fetches dns zone with a given domain", func() {
			dns := client.RetrieveDNS("the-domain")

			Expect(dns.ID).To(Equal("the-id"))
			Expect(dns.NameServers).To(Equal("ns1,ns2"))

			Expect(route53Client.ListHostedZonesByNameCall.Receives.Input).To(Equal(&awsroute53.ListHostedZonesByNameInput{
				DNSName: awslib.String("the-domain"),
			}))
			Expect(route53Client.GetHostedZoneCall.Receives.Input).To(Equal(&awsroute53.GetHostedZoneInput{
				Id: awslib.String("the-id"),
			}))
		})

		Context("when no dns zone at that domain exists", func() {
			BeforeEach(func() {
				route53Client.ListHostedZonesByNameCall.Returns.Output = &awsroute53.ListHostedZonesByNameOutput{
					HostedZones: []*awsroute53.HostedZone{},
				}
			})

			It("returns an empty struct", func() {
				dns := client.RetrieveDNS("the-domain")

				Expect(dns.ID).To(Equal(""))
				Expect(dns.NameServers).To(Equal(""))

				Expect(route53Client.GetHostedZoneCall.CallCount).To(Equal(0))
			})
		})

		Describe("failure cases", func() {
			Context("when hosted zones cannot be listed", func() {
				BeforeEach(func() {
					route53Client.ListHostedZonesByNameCall.Returns.Error = errors.New("feijoa")
				})

				It("returns empty struct", func() {
					dns := client.RetrieveDNS("the-domain")

					Expect(dns.ID).To(Equal(""))
					Expect(dns.NameServers).To(Equal(""))
				})
			})

			Context("when the hosted zone cannot be described", func() {
				BeforeEach(func() {
					route53Client.GetHostedZoneCall.Returns.Error = errors.New("guava")
				})

				It("returns empty struct", func() {
					dns := client.RetrieveDNS("the-domain")

					Expect(dns.ID).To(Equal(""))
					Expect(dns.NameServers).To(Equal(""))
				})
			})
		})
	})

	Describe("RetrieveAZs", func() {
		var (
			client    aws.Client
			ec2Client *fakes.AWSEC2Client
		)

		BeforeEach(func() {
			ec2Client = &fakes.AWSEC2Client{}
			client = aws.NewClientWithInjectedEC2Client(ec2Client, &fakes.Logger{})
		})

		It("fetches availability zones for a given region", func() {
			ec2Client.DescribeAvailabilityZonesCall.Returns.Output = &awsec2.DescribeAvailabilityZonesOutput{
				AvailabilityZones: []*awsec2.AvailabilityZone{
					{ZoneName: awslib.String("us-east-1a")},
					{ZoneName: awslib.String("us-east-1b")},
					{ZoneName: awslib.String("us-east-1e")},
					{ZoneName: awslib.String("us-east-1c")},
				},
			}

			azs, err := client.RetrieveAZs("us-east-1")

			Expect(err).NotTo(HaveOccurred())
			Expect(azs).To(Equal([]string{"us-east-1a", "us-east-1b", "us-east-1c", "us-east-1e"}))
			Expect(ec2Client.DescribeAvailabilityZonesCall.Receives.Input).To(Equal(&awsec2.DescribeAvailabilityZonesInput{
				Filters: []*awsec2.Filter{{
					Name:   awslib.String("region-name"),
					Values: []*string{awslib.String("us-east-1")},
				}},
			}))
		})

		Describe("failure cases", func() {
			Context("when AWS returns a nil availability zone", func() {
				BeforeEach(func() {
					ec2Client.DescribeAvailabilityZonesCall.Returns.Output = &awsec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []*awsec2.AvailabilityZone{nil},
					}
				})

				It("returns an error", func() {
					_, err := client.RetrieveAZs("us-east-1")
					Expect(err).To(MatchError("aws returned nil availability zone"))
				})
			})

			Context("when an availability zone with a nil ZoneName", func() {
				BeforeEach(func() {
					ec2Client.DescribeAvailabilityZonesCall.Returns.Output = &awsec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []*awsec2.AvailabilityZone{{ZoneName: nil}},
					}
				})

				It("returns an error", func() {
					_, err := client.RetrieveAZs("us-east-1")
					Expect(err).To(MatchError("aws returned availability zone with nil zone name"))
				})
			})

			Context("when describe availability zones fails", func() {
				BeforeEach(func() {
					ec2Client.DescribeAvailabilityZonesCall.Returns.Error = errors.New("describe availability zones failed")
				})

				It("returns an error", func() {
					_, err := client.RetrieveAZs("us-east-1")
					Expect(err).To(MatchError("describe availability zones failed"))
				})
			})
		})
	})

	Describe("ValidateSafeToDelete", func() {
		var (
			client    aws.Client
			ec2Client *fakes.AWSEC2Client
		)

		BeforeEach(func() {
			ec2Client = &fakes.AWSEC2Client{}
			client = aws.NewClientWithInjectedEC2Client(ec2Client, &fakes.Logger{})
		})

		Context("when the only EC2 instances are bosh and nat", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
					Reservations: []*awsec2.Reservation{
						reservationContainingInstance("NAT"),
						reservationContainingInstance("bosh/0"),
					},
				}
			})

			It("returns nil", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).NotTo(HaveOccurred())

				Expect(ec2Client.DescribeInstancesCall.Receives.Input).To(Equal(&awsec2.DescribeInstancesInput{
					Filters: []*awsec2.Filter{{
						Name:   awslib.String("vpc-id"),
						Values: []*string{awslib.String("some-vpc-id")},
					}},
				}))
			})
		})

		Context("when passed an environment ID", func() {
			Context("when the only EC2 instances are bosh, jumpbox and envID-nat", func() {
				BeforeEach(func() {
					ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
						Reservations: []*awsec2.Reservation{
							reservationContainingInstance("example-env-id-nat"),
							reservationContainingInstance("bosh/0"),
							reservationContainingInstance("jumpbox/0"),
						},
					}
				})

				It("returns nil", func() {
					err := client.ValidateSafeToDelete("some-vpc-id", "example-env-id")
					Expect(err).NotTo(HaveOccurred())

					Expect(ec2Client.DescribeInstancesCall.Receives.Input).To(Equal(&awsec2.DescribeInstancesInput{
						Filters: []*awsec2.Filter{{
							Name:   awslib.String("vpc-id"),
							Values: []*string{awslib.String("some-vpc-id")},
						}},
					}))
				})
			})
		})

		Context("when there are no instances at all", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{Reservations: []*awsec2.Reservation{}}
			})

			It("returns nil", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when there are bosh-deployed VMs in the VPC", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
					Reservations: []*awsec2.Reservation{
						reservationContainingInstance("NAT"),
						reservationContainingInstance("bosh/0"),
						reservationContainingInstance("first-bosh-deployed-vm"),
						reservationContainingInstance("second-bosh-deployed-vm"),
					},
				}
			})

			It("returns an error", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).To(MatchError("vpc some-vpc-id is not safe to delete; vms still exist: [first-bosh-deployed-vm, second-bosh-deployed-vm]"))
			})
		})

		Context("even when there are two VMs in the VPC, but they are not NAT and BOSH", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
					Reservations: []*awsec2.Reservation{
						reservationContainingInstance("not-bosh"),
						reservationContainingInstance("not-nat"),
					},
				}
			})

			It("returns an error", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).To(MatchError("vpc some-vpc-id is not safe to delete; vms still exist: [not-bosh, not-nat]"))
			})
		})

		Context("even if the vpc contains other instances tagged NAT and bosh/0", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
					Reservations: []*awsec2.Reservation{
						reservationContainingInstance("NAT"),
						reservationContainingInstance("NAT"),
						reservationContainingInstance("bosh/0"),
						reservationContainingInstance("bosh/0"),
						reservationContainingInstance("bosh/0"),
					},
				}
			})

			It("returns an error", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).To(MatchError("vpc some-vpc-id is not safe to delete; vms still exist: [NAT, bosh/0, bosh/0]"))
			})
		})

		Context("even if the vpc contains untagged vms", func() {
			BeforeEach(func() {
				ec2Client.DescribeInstancesCall.Returns.Output = &awsec2.DescribeInstancesOutput{
					Reservations: []*awsec2.Reservation{
						{
							Instances: []*awsec2.Instance{{
								Tags: []*awsec2.Tag{{
									Key:   awslib.String("Name"),
									Value: awslib.String(""),
								}},
							}, {}, {}},
						},
					},
				}
			})

			It("returns an error", func() {
				err := client.ValidateSafeToDelete("some-vpc-id", "")
				Expect(err).To(MatchError("vpc some-vpc-id is not safe to delete; vms still exist: [unnamed, unnamed, unnamed]"))
			})
		})

		Describe("failure cases", func() {
			Context("when the describe instances call fails", func() {
				BeforeEach(func() {
					ec2Client.DescribeInstancesCall.Returns.Error = errors.New("failed to describe instances")
				})

				It("returns an error", func() {
					err := client.ValidateSafeToDelete("some-vpc-id", "")
					Expect(err).To(MatchError("failed to describe instances"))
				})
			})
		})
	})
})

func reservationContainingInstance(tag string) *awsec2.Reservation {
	return &awsec2.Reservation{
		Instances: []*awsec2.Instance{{
			Tags: []*awsec2.Tag{{
				Key:   awslib.String("Name"),
				Value: awslib.String(tag),
			}},
		}},
	}
}
