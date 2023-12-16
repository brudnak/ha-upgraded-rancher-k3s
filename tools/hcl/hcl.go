package hcl

import (
	"fmt"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"log"
	"os"
	"strings"
)

const haFile = "../../../../ha.yml"

func RancherHelm(url, repositoryUrl, password, rancherVersion, imageTag, filePath string, pspBool bool) {
	f := hclwrite.NewEmptyFile()

	tfVarsFile, err := os.Create(filePath)
	if err != nil {
		fmt.Println(err)
		return
	}

	rootBody := f.Body()

	rootBody.SetAttributeValue("rancher_url", cty.StringVal(url))
	rootBody.SetAttributeValue("repository_url", cty.StringVal(repositoryUrl))
	rootBody.SetAttributeValue("bootstrap_password", cty.StringVal(password))
	rootBody.SetAttributeValue("rancher_version", cty.StringVal(rancherVersion))
	rootBody.SetAttributeValue("image_tag", cty.StringVal(imageTag))
	if pspBool == false {
		rootBody.SetAttributeValue("psp_bool", cty.BoolVal(pspBool))
	}

	_, err = tfVarsFile.Write(f.Bytes())
	if err != nil {
		fmt.Println(err)
		return
	}
}

func GenAwsVar(
	accessKey,
	secretKey,
	awsPrefix,
	awsVpc,
	subnetA,
	subnetB,
	subnetC,
	awsAmi,
	subnetId,
	securityGroupId,
	pemKeyName,
	awsRdsPassword,
	route53Fqdn,
	instanceTypeSize string) {

	f := hclwrite.NewEmptyFile()

	tfVarsFile, err := os.Create("../../terratest/modules/aws/terraform.tfvars")
	if err != nil {
		fmt.Println(err)
		return
	}

	rootBody := f.Body()

	rootBody.SetAttributeValue("aws_access_key", cty.StringVal(accessKey))
	rootBody.SetAttributeValue("aws_secret_key", cty.StringVal(secretKey))
	rootBody.SetAttributeValue("aws_prefix", cty.StringVal(awsPrefix))
	rootBody.SetAttributeValue("aws_vpc", cty.StringVal(awsVpc))
	rootBody.SetAttributeValue("aws_subnet_a", cty.StringVal(subnetA))
	rootBody.SetAttributeValue("aws_subnet_b", cty.StringVal(subnetB))
	rootBody.SetAttributeValue("aws_subnet_c", cty.StringVal(subnetC))
	rootBody.SetAttributeValue("aws_ami", cty.StringVal(awsAmi))
	rootBody.SetAttributeValue("aws_subnet_id", cty.StringVal(subnetId))
	rootBody.SetAttributeValue("aws_security_group_id", cty.StringVal(securityGroupId))
	rootBody.SetAttributeValue("aws_pem_key_name", cty.StringVal(pemKeyName))
	rootBody.SetAttributeValue("aws_rds_password", cty.StringVal(awsRdsPassword))
	rootBody.SetAttributeValue("aws_route53_fqdn", cty.StringVal(route53Fqdn))
	rootBody.SetAttributeValue("aws_ec2_instance_type", cty.StringVal(instanceTypeSize))

	_, err = tfVarsFile.Write(f.Bytes())
	if err != nil {
		fmt.Println(err)
		return
	}
}

func CreateMainTf(filePath string) {
	f := hclwrite.NewEmptyFile()
	rootBody := f.Body()

	// Terraform block with required providers
	terraformBlock := rootBody.AppendNewBlock("terraform", nil)
	requiredProvidersBlock := terraformBlock.Body().AppendNewBlock("required_providers", nil)
	_ = requiredProvidersBlock.Body().SetAttributeValue("helm", cty.ObjectVal(map[string]cty.Value{
		"source":  cty.StringVal("hashicorp/helm"),
		"version": cty.StringVal("2.7.1"),
	}))

	// Provider block
	providerBlock := rootBody.AppendNewBlock("provider", []string{"helm"}).Body()
	kubernetesBlock := providerBlock.AppendNewBlock("kubernetes", nil).Body()
	kubernetesBlock.SetAttributeValue("config_path", cty.StringVal("../../../../ha.yml"))

	// Resource block for helm_release
	resourceBlock := rootBody.AppendNewBlock("resource", []string{"helm_release", "rancher"}).Body()
	resourceBlock.SetAttributeValue("name", cty.StringVal("rancher"))
	resourceBlock.SetAttributeValue("repository", cty.StringVal("${var.repository_url}"))
	resourceBlock.SetAttributeValue("chart", cty.StringVal("rancher"))
	resourceBlock.SetAttributeValue("version", cty.StringVal("${var.rancher_version}"))
	resourceBlock.SetAttributeValue("create_namespace", cty.BoolVal(true))
	resourceBlock.SetAttributeValue("namespace", cty.StringVal("cattle-system"))

	// Set blocks
	setParams := []struct {
		Name, Value string
	}{
		{"hostname", "${var.rancher_url}"},
		{"global.cattle.psp.enabled", "${var.psp_bool}"},
		{"rancherImageTag", "${var.image_tag}"},
		{"bootstrapPassword", "${var.bootstrap_password}"},
		{"tls", "external"},
	}

	for _, param := range setParams {
		setBlock := resourceBlock.AppendNewBlock("set", nil).Body()
		setBlock.SetAttributeValue("name", cty.StringVal(param.Name))
		setBlock.SetAttributeValue("value", cty.StringVal(param.Value))
	}

	// Write to file
	err := os.WriteFile(filePath, f.Bytes(), 0644)
	if err != nil {
		fmt.Println(err)
		return
	}

	fixFormatting()
}

func CreateVariablesTf(filePath string) {
	f := hclwrite.NewEmptyFile()
	rootBody := f.Body()

	// Define the variables
	variables := []string{"rancher_url", "repository_url", "bootstrap_password", "rancher_version", "image_tag"}
	for _, variable := range variables {
		rootBody.AppendNewBlock("variable", []string{variable})
		// No additional configuration needed for these variables
	}

	// Special case for psp_bool with a default value
	pspBoolBlock := rootBody.AppendNewBlock("variable", []string{"psp_bool"})
	pspBoolBlock.Body().SetAttributeValue("default", cty.BoolVal(false))

	// Write to file
	err := os.WriteFile(filePath, f.Bytes(), 0644)
	if err != nil {
		fmt.Println(err)
		return
	}
}

func fixFormatting() {
	bytes, err := os.ReadFile("../../terratest/modules/helm/ha/main.tf")
	if err != nil {
		log.Fatalf("Failed to open file: %s", err)
	}

	// Convert to string
	contents := string(bytes)

	// Replace $$ with $
	newContents := strings.ReplaceAll(contents, "$$", "$")

	// Write the contents back to the file
	err = os.WriteFile("../../terratest/modules/helm/ha/main.tf", []byte(newContents), 0644)
	if err != nil {
		log.Fatalf("Failed to write to file: %s", err)
	}
}
