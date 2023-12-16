package test

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	toolkit "github.com/brudnak/ha-upgraded-rancher-k3s/tools"
	"github.com/brudnak/ha-upgraded-rancher-k3s/tools/hcl"
	"github.com/spf13/viper"
	"log"
	"os"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
)

var haUrl string
var password string
var configIp string

var tools toolkit.Tools

func TestCreateHAUpgradedRancher(t *testing.T) {

	err := checkS3ObjectExists("terraform.tfstate")
	if err != nil {
		log.Fatal("Error checking if tfstate exists in s3:", err)
	}

	createAWSVar()
	os.Setenv("AWS_ACCESS_KEY_ID", viper.GetString("tf_vars.aws_access_key"))
	os.Setenv("AWS_SECRET_ACCESS_KEY", viper.GetString("tf_vars.aws_secret_key"))
	terraformOptions := &terraform.Options{
		TerraformDir: "../modules/aws",
		NoColor:      true,
		BackendConfig: map[string]interface{}{
			"bucket": viper.GetString("s3.bucket"),
			"key":    "terraform.tfstate",
			"region": viper.GetString("s3.region"),
		},
	}

	terraform.InitAndApply(t, terraformOptions)

	infra1Server1IPAddress := terraform.Output(t, terraformOptions, "infra1_server1_ip")
	infra1Server2IPAddress := terraform.Output(t, terraformOptions, "infra1_server2_ip")
	infra1MysqlEndpoint := terraform.Output(t, terraformOptions, "infra1_mysql_endpoint")
	infra1MysqlPassword := terraform.Output(t, terraformOptions, "infra1_mysql_password")
	infra1RancherURL := terraform.Output(t, terraformOptions, "infra1_rancher_url")

	noneOneIPAddressValidationResult := tools.CheckIPAddress(infra1Server1IPAddress)
	nodeTwoIPAddressValidationResult := tools.CheckIPAddress(infra1Server2IPAddress)

	assert.Equal(t, "valid", noneOneIPAddressValidationResult)
	assert.Equal(t, "valid", nodeTwoIPAddressValidationResult)

	haConfig := toolkit.K3SConfig{
		DBPassword: infra1MysqlPassword,
		DBEndpoint: infra1MysqlEndpoint,
		RancherURL: infra1RancherURL,
		Node1IP:    infra1Server1IPAddress,
		Node2IP:    infra1Server2IPAddress,
	}

	tools.HAInstallK3S(haConfig)

	t.Run("Setup HA Rancher", TestSetupHARancher)
	t.Run("Upgrade HA Rancher", TestUpgradeHARancher)

	haUrl = infra1RancherURL
	password = viper.GetString("rancher.bootstrap_password")

	log.Printf("K3s HA Rancher https://%s", infra1RancherURL)

}

func TestSetupHARancher(t *testing.T) {

	terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{

		TerraformDir: "../modules/helm/ha",
		NoColor:      true,
	})

	terraform.InitAndApply(t, terraformOptions)
}

func TestUpgradeHARancher(t *testing.T) {

	cleanupFiles("../modules/helm/ha/terraform.tfvars")
	originalPath := "../modules/helm/ha/upgrade.tfvars"
	newPath := "../modules/helm/ha/terraform.tfvars"
	e := os.Rename(originalPath, newPath)
	if e != nil {
		log.Fatal(e)
	}

	terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{

		TerraformDir: "../modules/helm/ha",
		NoColor:      true,
	})

	terraform.InitAndApply(t, terraformOptions)
}

func TestJenkinsCleanup(t *testing.T) {
	createAWSVar()
	os.Setenv("AWS_ACCESS_KEY_ID", viper.GetString("tf_vars.aws_access_key"))
	os.Setenv("AWS_SECRET_ACCESS_KEY", viper.GetString("tf_vars.aws_secret_key"))
	terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
		TerraformDir: "../modules/aws",
		NoColor:      true,
		BackendConfig: map[string]interface{}{
			"bucket": viper.GetString("s3.bucket"),
			"key":    "terraform.tfstate",
			"region": viper.GetString("s3.region"),
		},
	})
	terraform.Init(t, terraformOptions)
	terraform.Destroy(t, terraformOptions)
	defer deleteS3Object(viper.GetString("s3.bucket"), "terraform.tfstate")
}

func TestHACleanup(t *testing.T) {
	terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
		TerraformDir: "../modules/aws",
		NoColor:      true,
	})

	terraform.Destroy(t, terraformOptions)

	filepaths := []string{
		"../../ha.yml",
		"../modules/helm/ha/main.tf",
		"../modules/helm/ha/variables.tf",
		"../modules/helm/ha/.terraform.lock.hcl",
		"../modules/helm/ha/terraform.tfstate",
		"../modules/helm/ha/terraform.tfstate.backup",
		"../modules/helm/ha/terraform.tfvars",
		"../modules/helm/ha/upgrade.tfvars",
		"../modules/kubectl/.terraform.lock.hcl",
		"../modules/kubectl/terraform.tfstate",
		"../modules/kubectl/terraform.tfstate.backup",
		"../modules/kubectl/terraform.tfvars",
		"../modules/kubectl/theconfig.yml",
		"../modules/aws/.terraform.lock.hcl",
		"../modules/aws/terraform.tfstate",
		"../modules/aws/terraform.tfstate.backup",
		"../modules/aws/terraform.tfvars",
	}

	folderpaths := []string{
		"../modules/helm/ha/.terraform",
		"../modules/kubectl/.terraform",
		"../modules/aws/.terraform",
	}

	cleanupFiles(filepaths...)
	cleanupFolders(folderpaths...)

	viper.AddConfigPath("../../")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()
	if err != nil {
		log.Println("error reading config:", err)
	}
	deleteS3Object(viper.GetString("s3.bucket"), "terraform.tfstate")
}

func cleanupFiles(paths ...string) {
	for _, path := range paths {
		err := tools.RemoveFile(path)
		if err != nil {
			log.Println("error removing file", err)
		}
	}
}

func cleanupFolders(paths ...string) {
	for _, path := range paths {
		err := tools.RemoveFolder(path)
		if err != nil {
			log.Println("error removing folder", err)
		}
	}
}

func createAWSVar() {
	viper.AddConfigPath("../../")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()

	if err != nil {
		log.Println("error reading config:", err)
	}

	hcl.GenAwsVar(
		viper.GetString("tf_vars.aws_access_key"),
		viper.GetString("tf_vars.aws_secret_key"),
		viper.GetString("tf_vars.aws_prefix"),
		viper.GetString("tf_vars.aws_vpc"),
		viper.GetString("tf_vars.aws_subnet_a"),
		viper.GetString("tf_vars.aws_subnet_b"),
		viper.GetString("tf_vars.aws_subnet_c"),
		viper.GetString("tf_vars.aws_ami"),
		viper.GetString("tf_vars.aws_subnet_id"),
		viper.GetString("tf_vars.aws_security_group_id"),
		viper.GetString("tf_vars.aws_pem_key_name"),
		viper.GetString("tf_vars.aws_rds_password"),
		viper.GetString("tf_vars.aws_route53_fqdn"),
		viper.GetString("tf_vars.aws_ec2_instance_type"),
	)
}

// deleteS3Object deletes an object from a specified S3 bucket
func deleteS3Object(bucket string, item string) error {

	viper.AddConfigPath("../../")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()

	os.Setenv("AWS_ACCESS_KEY_ID", viper.GetString("tf_vars.aws_access_key"))
	os.Setenv("AWS_SECRET_ACCESS_KEY", viper.GetString("tf_vars.aws_secret_key"))

	if err != nil {
		log.Println("error reading config:", err)
	}

	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String(viper.GetString("s3.region"))},
	)

	svc := s3.New(sess)

	_, err = svc.DeleteObject(&s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(item)})
	if err != nil {
		return err
	}

	err = svc.WaitUntilObjectNotExists(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(item),
	})
	if err != nil {
		return err
	}

	return nil
}

// deleteS3Object deletes an object from a specified S3 bucket
func TestDeleteS3Object(t *testing.T) {

	bucket := "atb-tf-bucket"
	item := "terraform.tfstate"
	viper.AddConfigPath("../../")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()

	os.Setenv("AWS_ACCESS_KEY_ID", viper.GetString("tf_vars.aws_access_key"))
	os.Setenv("AWS_SECRET_ACCESS_KEY", viper.GetString("tf_vars.aws_secret_key"))

	if err != nil {
		log.Println("error reading config:", err)
	}

	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String(viper.GetString("s3.region"))},
	)

	svc := s3.New(sess)

	_, err = svc.DeleteObject(&s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(item)})
	if err != nil {
		log.Println(err)
	}

	err = svc.WaitUntilObjectNotExists(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(item),
	})
	if err != nil {
		log.Println(err)
	}
}

func checkS3ObjectExists(item string) error {
	viper.AddConfigPath("../../")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	err := viper.ReadInConfig()

	os.Setenv("AWS_ACCESS_KEY_ID", viper.GetString("tf_vars.aws_access_key"))
	os.Setenv("AWS_SECRET_ACCESS_KEY", viper.GetString("tf_vars.aws_secret_key"))

	if err != nil {
		log.Println("error reading config:", err)
	}

	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String(viper.GetString("s3.region"))},
	)

	bucket := viper.GetString("s3.bucket")

	svc := s3.New(sess)

	_, err = svc.HeadObject(&s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(item)})
	if err != nil {
		// If the error is due to the file not existing, that's fine, and we return nil.
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey, "NotFound":
				return nil
			}
		}
		// Otherwise, we return the error as it might be due to a network issue or something else.
		return err
	}

	// If we get to this point, it means the file exists, so we log an error message and exit the program.
	log.Fatalf("A tfstate file already exists in bucket %s. Please clean up the old HA K3s environment before creating a new one.", bucket)
	return nil
}
