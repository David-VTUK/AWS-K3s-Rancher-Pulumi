package main

import (
	"fmt"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/rds"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"strconv"
)

/*
Create the following

VPC
Subnets

RDS instance


*/

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		// Create AWS VPC
		vpc, err := ec2.NewVpc(ctx, "david-pulumi-vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String("10.0.0.0/16"),
			Tags:               pulumi.StringMap{"Name": pulumi.String("david-pulumi-vpc")},
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
		})

		if err != nil {
			return err
		}
		// Create IGW
		igw, err := ec2.NewInternetGateway(ctx, "david-pulumi-gw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
		})

		if err != nil {
			return err
		}

		// Create AWS security group
		sg, err := ec2.NewSecurityGroup(ctx, "david-pulumi-sg", &ec2.SecurityGroupArgs{
			Description: pulumi.String("Security group for ec2 Nodes"),
			Name:        pulumi.String("david-pulumi-sg"),
			VpcId:       vpc.ID(),

			Ingress: ec2.SecurityGroupIngressArray{
				ec2.SecurityGroupIngressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
				},
			},
		})

		if err != nil {
			return err
		}

		// Get the list of AZ's for the defined region
		azState := "available"
		zoneList, err := aws.GetAvailabilityZones(ctx, &aws.GetAvailabilityZonesArgs{
			State: &azState,
		})

		if err != nil {
			return err
		}

		//How many AZ's to spread nodes across. Default to 3.
		zoneNumber := 3

		var subnets []*ec2.Subnet

		// Iterate through the AZ's for the VPC and create a subnet in each
		for i := 0; i < zoneNumber; i++ {
			subnet, err := ec2.NewSubnet(ctx, "david-pulumi-subnet-"+strconv.Itoa(i), &ec2.SubnetArgs{
				AvailabilityZone:    pulumi.String(zoneList.Names[i]),
				Tags:                pulumi.StringMap{"Name": pulumi.String("david-pulumi-subnet-" + strconv.Itoa(i))},
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String("10.0." + strconv.Itoa(i) + ".0/24"),
				MapPublicIpOnLaunch: pulumi.Bool(true),
			})

			if err != nil {
				return err
			}

			subnets = append(subnets, subnet)
		}

		// Add Route Table
		_, err = ec2.NewDefaultRouteTable(ctx, "david-pulumi-routetable", &ec2.DefaultRouteTableArgs{
			DefaultRouteTableId: vpc.DefaultRouteTableId,
			Routes: ec2.DefaultRouteTableRouteArray{
				ec2.DefaultRouteTableRouteInput(&ec2.DefaultRouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				}),
			},
		})

		if err != nil {
			return err
		}

		subnetGroup, err := rds.NewSubnetGroup(ctx, "rds-subnet-group", &rds.SubnetGroupArgs{
			Name:      pulumi.String("rds-subnet-group"),
			SubnetIds: pulumi.StringArray{subnets[0].ID(), subnets[1].ID(), subnets[2].ID()},
		})

		if err != nil {
			return err
		}

		rdsInstance, err := rds.NewInstance(ctx, "_default", &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(10),
			Engine:              pulumi.String("mysql"),
			EngineVersion:       pulumi.String("8.0.25"),
			InstanceClass:       pulumi.String("db.t3.micro"),
			Name:                pulumi.String("mydb"),
			ParameterGroupName:  pulumi.String("default.mysql8.0"),
			Password:            pulumi.String("foobarbaz"),
			SkipFinalSnapshot:   pulumi.Bool(true),
			Username:            pulumi.String("foo"),
			Identifier:          pulumi.String("k3s-demo-cluster"),
			VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
			MultiAz:             pulumi.Bool(true),
			DbSubnetGroupName:   subnetGroup.Name,
		})

		if err != nil {
			return err
		}

		// Need to jitter the EC2 creations because of https://github.com/k3s-io/k3s/issues/3226

		userdata := rdsInstance.Endpoint.ApplyT(func(endpoint string) string {
			getPublicIP := "IP=$(curl -H \"X-aws-ec2-metadata-token: $TOKEN\" -v http://169.254.169.254/latest/meta-data/public-ipv4)"
			installK3s := fmt.Sprintf("curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.20.8+k3s1 K3S_TOKEN=\"super-secret\" INSTALL_K3S_EXEC=\"--node-external-ip $IP\" sh -s - server --datastore-endpoint=\"mysql://foo:foobarbaz@tcp(%s)/mydb\"", endpoint)

			generatedUserData := fmt.Sprintf("#!/bin/bash\n%s\n%s", getPublicIP, installK3s)
			return generatedUserData
		}).(pulumi.StringOutput)

		k3snode1, err := ec2.NewInstance(ctx, "david-pulumi-fleet-node-1", &ec2.InstanceArgs{
			Ami:                 pulumi.String("ami-0ff4c8fb495a5a50d"),
			InstanceType:        pulumi.String("t2.xlarge"),
			Tags:                pulumi.StringMap{"Name": pulumi.String("david-k3s-node-2")},
			KeyName:             pulumi.String("davidh-keypair"),
			VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
			UserData:            userdata,
			SubnetId:            subnets[0].ID(),
		})

		_, err = ec2.NewInstance(ctx, "david-pulumi-fleet-node-2", &ec2.InstanceArgs{
			Ami:                 pulumi.String("ami-0ff4c8fb495a5a50d"),
			InstanceType:        pulumi.String("t2.xlarge"),
			Tags:                pulumi.StringMap{"Name": pulumi.String("david-k3s-node-2")},
			KeyName:             pulumi.String("davidh-keypair"),
			VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
			UserData:            userdata,
			SubnetId:            subnets[1].ID(),
		}, pulumi.DependsOn([]pulumi.Resource{k3snode1}))

		if err != nil {
			return err
		}

		return nil
	})
}
