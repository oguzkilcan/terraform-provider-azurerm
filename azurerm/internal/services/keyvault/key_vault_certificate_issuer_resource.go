package keyvault

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/keyvault/v7.1/keyvault"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/clients"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/services/keyvault/parse"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/services/keyvault/validate"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/tf/pluginsdk"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/tf/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/timeouts"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

func resourceKeyVaultCertificateIssuer() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceKeyVaultCertificateIssuerCreateOrUpdate,
		Update: resourceKeyVaultCertificateIssuerCreateOrUpdate,
		Read:   resourceKeyVaultCertificateIssuerRead,
		Delete: resourceKeyVaultCertificateIssuerDelete,
		Importer: &schema.ResourceImporter{
			State: nestedItemResourceImporter,
		},

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*pluginsdk.Schema{
			"key_vault_id": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.VaultID,
			},

			"name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.CertificateIssuerName,
			},

			"provider_name": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringInSlice([]string{
					"DigiCert",
					"GlobalSign",
					"OneCertV2-PrivateCA",
					"OneCertV2-PublicCA",
					"SslAdminV2",
				}, false),
			},

			"org_id": {
				Type:         pluginsdk.TypeString,
				Optional:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"account_id": {
				Type:     pluginsdk.TypeString,
				Optional: true,
			},

			"admin": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"email_address": {
							Type:     pluginsdk.TypeString,
							Required: true,
						},
						"first_name": {
							Type:     pluginsdk.TypeString,
							Optional: true,
						},
						"last_name": {
							Type:     pluginsdk.TypeString,
							Optional: true,
						},
						"phone": {
							Type:     pluginsdk.TypeString,
							Optional: true,
						},
					},
				},
			},

			"password": {
				Type:      pluginsdk.TypeString,
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func resourceKeyVaultCertificateIssuerCreateOrUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	keyVaultsClient := meta.(*clients.Client).KeyVault
	client := meta.(*clients.Client).KeyVault.ManagementClient
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	keyVaultId, err := parse.VaultID(d.Get("key_vault_id").(string))
	if err != nil {
		return err
	}

	keyVaultBaseUri, err := keyVaultsClient.BaseUriForKeyVault(ctx, *keyVaultId)
	if err != nil {
		return fmt.Errorf("retrieving base uri for %s: %+v", *keyVaultId, err)
	}

	if d.IsNewResource() {
		existing, err := client.GetCertificateIssuer(ctx, *keyVaultBaseUri, name)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("failed to check for presence of existing Certificate Issuer %q (Key Vault %q): %s", name, *keyVaultBaseUri, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_key_vault_certificate_issuer", *existing.ID)
		}
	}

	parameter := keyvault.CertificateIssuerSetParameters{
		Provider:            utils.String(d.Get("provider_name").(string)),
		OrganizationDetails: &keyvault.OrganizationDetails{},
	}

	if orgIdRaw, ok := d.GetOk("org_id"); ok {
		parameter.OrganizationDetails.ID = utils.String(orgIdRaw.(string))
	}

	if adminsRaw, ok := d.GetOk("admin"); ok {
		parameter.OrganizationDetails.AdminDetails = expandKeyVaultCertificateIssuerOrganizationDetailsAdminDetails(adminsRaw.([]interface{}))
	}

	accountId, gotAccountId := d.GetOk("account_id")
	password, gotPassword := d.GetOk("password")

	if gotAccountId && gotPassword {
		parameter.Credentials = &keyvault.IssuerCredentials{
			AccountID: utils.String(accountId.(string)),
			Password:  utils.String(password.(string)),
		}
	}

	if _, err = client.SetCertificateIssuer(ctx, *keyVaultBaseUri, name, parameter); err != nil {
		return fmt.Errorf("failed to set Certificate Issuer %q (Key Vault %q): %s", name, keyVaultId, err)
	}

	resp, err := client.GetCertificateIssuer(ctx, *keyVaultBaseUri, name)
	if err != nil {
		return err
	}

	if resp.ID == nil || *resp.ID == "" {
		return fmt.Errorf("failure reading Key Vault Certificate Issuer ID for %q", name)
	}
	d.SetId(*resp.ID)

	return resourceKeyVaultCertificateIssuerRead(d, meta)
}

func resourceKeyVaultCertificateIssuerRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).KeyVault.ManagementClient
	keyVaultsClient := meta.(*clients.Client).KeyVault
	resourcesClient := meta.(*clients.Client).Resource
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.IssuerID(d.Id())
	if err != nil {
		return err
	}

	keyVaultIdRaw, err := keyVaultsClient.KeyVaultIDFromBaseUrl(ctx, resourcesClient, id.KeyVaultBaseUrl)
	if err != nil {
		return fmt.Errorf("Error retrieving the Resource ID the Key Vault at URL %q: %s", id.KeyVaultBaseUrl, err)
	}
	if keyVaultIdRaw == nil {
		log.Printf("[DEBUG] Unable to determine the Resource ID for the Key Vault at URL %q - removing from state!", id.KeyVaultBaseUrl)
		d.SetId("")
		return nil
	}

	keyVaultId, err := parse.VaultID(*keyVaultIdRaw)
	if err != nil {
		return err
	}

	ok, err := keyVaultsClient.Exists(ctx, *keyVaultId)
	if err != nil {
		return fmt.Errorf("checking if %s for Certificate %q exists: %v", *keyVaultId, id.Name, err)
	}
	if !ok {
		log.Printf("[DEBUG] Certificate %q was not found %s - removing from state", id.Name, *keyVaultId)
		d.SetId("")
		return nil
	}

	resp, err := client.GetCertificateIssuer(ctx, id.KeyVaultBaseUrl, id.Name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] KeyVault Certificate Issuer %q (KeyVault URI %q) does not exist - removing from state", id.Name, id.KeyVaultBaseUrl)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("failed to make Read request on Azure KeyVault Certificate Issuer %s: %+v", id.Name, err)
	}

	d.Set("name", id.Name)

	if resp.Provider != nil {
		d.Set("provider_name", resp.Provider)
	}
	if resp.OrganizationDetails != nil {
		if resp.OrganizationDetails.ID != nil {
			d.Set("org_id", resp.OrganizationDetails.ID)
		}
		d.Set("admin", flattenKeyVaultCertificateIssuerAdmins(resp.OrganizationDetails.AdminDetails))
	}
	if resp.Credentials != nil {
		if resp.Credentials.AccountID != nil {
			d.Set("account_id", resp.Credentials.AccountID)
		}
	}

	return nil
}

func resourceKeyVaultCertificateIssuerDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).KeyVault.ManagementClient
	keyVaultsClient := meta.(*clients.Client).KeyVault
	resourcesClient := meta.(*clients.Client).Resource
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.IssuerID(d.Id())
	if err != nil {
		return err
	}

	// we verify it exists
	keyVaultIdRaw, err := keyVaultsClient.KeyVaultIDFromBaseUrl(ctx, resourcesClient, id.KeyVaultBaseUrl)
	if err != nil {
		return fmt.Errorf("retrieving the Resource ID the Key Vault at URL %q: %s", id.KeyVaultBaseUrl, err)
	}
	keyVaultId, err := parse.VaultID(*keyVaultIdRaw)
	if err != nil {
		return err
	}

	ok, err := keyVaultsClient.Exists(ctx, *keyVaultId)
	if err != nil {
		return fmt.Errorf("checking if key vault %q for Issuer %q in Vault at url %q exists: %v", *keyVaultId, id.Name, id.KeyVaultBaseUrl, err)
	}

	if !ok {
		log.Printf("[DEBUG] Issuer %q (Key Vault %q) was not found in Key Vault at URI %q - removing from state", id.Name, *keyVaultId, id.KeyVaultBaseUrl)
		d.SetId("")
		return nil
	}

	_, err = client.DeleteCertificateIssuer(ctx, id.KeyVaultBaseUrl, id.Name)
	return err
}

func expandKeyVaultCertificateIssuerOrganizationDetailsAdminDetails(vs []interface{}) *[]keyvault.AdministratorDetails {
	results := make([]keyvault.AdministratorDetails, 0, len(vs))

	for _, v := range vs {
		administratorDetails := keyvault.AdministratorDetails{}
		args := v.(map[string]interface{})
		if firstName, ok := args["first_name"]; ok {
			administratorDetails.FirstName = utils.String(firstName.(string))
		}
		if lastName, ok := args["last_name"]; ok {
			administratorDetails.LastName = utils.String(lastName.(string))
		}
		if emailAddress, ok := args["email_address"]; ok {
			administratorDetails.EmailAddress = utils.String(emailAddress.(string))
		}
		if phone, ok := args["phone"]; ok {
			administratorDetails.Phone = utils.String(phone.(string))
		}
		results = append(results, administratorDetails)
	}

	return &results
}

func flattenKeyVaultCertificateIssuerAdmins(input *[]keyvault.AdministratorDetails) []interface{} {
	results := make([]interface{}, 0)
	if input == nil {
		return results
	}

	for _, admin := range *input {
		emailAddress := ""
		if admin.EmailAddress != nil {
			emailAddress = *admin.EmailAddress
		}

		firstName := ""
		if admin.FirstName != nil {
			firstName = *admin.FirstName
		}

		lastName := ""
		if admin.LastName != nil {
			lastName = *admin.LastName
		}

		phone := ""
		if admin.Phone != nil {
			phone = *admin.Phone
		}

		results = append(results, map[string]interface{}{
			"email_address": emailAddress,
			"first_name":    firstName,
			"last_name":     lastName,
			"phone":         phone,
		})
	}

	return results
}
