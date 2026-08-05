# Arteria Multi-Tenant SaaS — Terraform (Azure)
# Provisions the shared platform infrastructure.
# Tenant-specific resources are created by the tenant provisioning API.

terraform {
  required_version = ">= 1.5"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }
  backend "azurerm" {
    resource_group_name  = "rg-arteria-tfstate"
    storage_account_name = "arteriaterraform"
    container_name       = "tfstate"
    key                  = "platform.tfstate"
  }
}

provider "azurerm" {
  features {}
}

variable "environment" {
  type    = string
  default = "prod"
}

variable "location" {
  type    = string
  default = "eastus"
}

variable "aks_node_count" {
  type    = number
  default = 3
}

variable "aks_spot_node_count" {
  type    = number
  default = 2
}

locals {
  prefix = "arteria-${var.environment}"
  tags = {
    Environment = var.environment
    Product     = "arteria"
    ManagedBy   = "terraform"
  }
}

# --- Resource Group ---
resource "azurerm_resource_group" "main" {
  name     = "rg-${local.prefix}"
  location = var.location
  tags     = local.tags
}

# --- AKS Cluster ---
resource "azurerm_kubernetes_cluster" "main" {
  name                = "aks-${local.prefix}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  dns_prefix          = local.prefix
  sku_tier            = "Standard"
  tags                = local.tags

  default_node_pool {
    name                = "system"
    vm_size             = "Standard_D4ds_v5"
    node_count          = var.aks_node_count
    os_disk_size_gb     = 128
    os_disk_type        = "Managed"
    type                = "VirtualMachineScaleSets"
    zones               = ["1", "2", "3"]
    auto_scaling_enabled = true
    min_count           = 2
    max_count           = 5
  }

  identity {
    type = "SystemAssigned"
  }

  network_profile {
    network_plugin = "azure"
    network_policy = "calico"
  }

  key_vault_secrets_provider {
    secret_rotation_enabled = true
  }

  oms_agent {
    log_analytics_workspace_id = azurerm_log_analytics_workspace.main.id
  }
}

# --- Spot Node Pool (for processing) ---
resource "azurerm_kubernetes_cluster_node_pool" "spot" {
  name                  = "spot"
  kubernetes_cluster_id = azurerm_kubernetes_cluster.main.id
  vm_size               = "Standard_D4ds_v5"
  priority              = "Spot"
  eviction_policy       = "Delete"
  spot_max_price        = -1
  node_count            = var.aks_spot_node_count
  os_disk_size_gb       = 64
  zones                 = ["1", "2", "3"]
  auto_scaling_enabled  = true
  min_count             = 0
  max_count             = 10
  node_taints           = ["kubernetes.azure.com/scalesetpriority=spot:NoSchedule"]
  node_labels           = { agentpool = "spot" }
  tags                  = local.tags
}

# --- Cosmos DB (Cassandra API) ---
resource "azurerm_cosmosdb_account" "main" {
  name                = "cosmos-${local.prefix}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  offer_type          = "Standard"
  kind                = "GlobalDocumentDB"
  tags                = local.tags

  capabilities {
    name = "EnableCassandra"
  }

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = azurerm_resource_group.main.location
    failover_priority = 0
  }

  # Per-tenant encryption via customer-managed keys
  # (enable when customer requires BYOK)
  # key_vault_key_id = azurerm_key_vault_key.cosmos.id
}

# --- Key Vault ---
resource "azurerm_key_vault" "main" {
  name                = "kv-${local.prefix}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tenant_id           = data.azurerm_client_config.current.tenant_id
  sku_name            = "standard"
  tags                = local.tags

  purge_protection_enabled   = true
  soft_delete_retention_days = 90

  # AKS managed identity gets secret access
  access_policy {
    tenant_id = data.azurerm_client_config.current.tenant_id
    object_id = azurerm_kubernetes_cluster.main.key_vault_secrets_provider[0].secret_identity[0].object_id

    secret_permissions = ["Get", "List"]
  }
}

# --- Container Registry ---
resource "azurerm_container_registry" "main" {
  name                = "arteriacr${var.environment}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  sku                 = "Standard"
  admin_enabled       = false
  tags                = local.tags
}

# AKS pull access to ACR
resource "azurerm_role_assignment" "aks_acr" {
  principal_id                     = azurerm_kubernetes_cluster.main.kubelet_identity[0].object_id
  role_definition_name             = "AcrPull"
  scope                            = azurerm_container_registry.main.id
  skip_service_principal_aad_check = true
}

# --- Log Analytics ---
resource "azurerm_log_analytics_workspace" "main" {
  name                = "law-${local.prefix}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  sku                 = "PerGB2018"
  retention_in_days   = 90
  tags                = local.tags
}

# --- Application Insights ---
resource "azurerm_application_insights" "main" {
  name                = "ai-${local.prefix}"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  workspace_id        = azurerm_log_analytics_workspace.main.id
  application_type    = "other"
  tags                = local.tags
}

# --- Front Door ---
resource "azurerm_cdn_frontdoor_profile" "main" {
  name                = "fd-${local.prefix}"
  resource_group_name = azurerm_resource_group.main.name
  sku_name            = "Standard_AzureFrontDoor"
  tags                = local.tags
}

data "azurerm_client_config" "current" {}

# --- Outputs ---
output "aks_cluster_name" {
  value = azurerm_kubernetes_cluster.main.name
}

output "cosmos_endpoint" {
  value = azurerm_cosmosdb_account.main.endpoint
}

output "acr_login_server" {
  value = azurerm_container_registry.main.login_server
}

output "key_vault_uri" {
  value = azurerm_key_vault.main.vault_uri
}

output "appinsights_connection_string" {
  value     = azurerm_application_insights.main.connection_string
  sensitive = true
}
