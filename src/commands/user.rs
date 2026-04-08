use anyhow::Result;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use crate::api::{CloudCentClient, Config};

#[derive(Debug, Clone)]
pub enum CallbackData {
    Pending,
    AuthReceived { cli_id: String, api_key: String },
    MetadataDownloaded,
    Failed(String),
}

#[allow(dead_code)]
pub struct InitResult {
    pub cli_id: String,
    pub api_key: String,
    pub message: String,
}

pub struct UserCommand {
    client: CloudCentClient,
}

impl UserCommand {
    pub fn new() -> Self {
        let mut client = CloudCentClient::new();
        let _ = client.load_config();
        Self { client }
    }

    pub fn from_client(client: CloudCentClient) -> Self {
        Self { client }
    }

    pub fn client(&self) -> &CloudCentClient {
        &self.client
    }

    pub fn client_mut(&mut self) -> &mut CloudCentClient {
        &mut self.client
    }

    pub fn is_initialized(&self) -> bool {
        self.client.get_config().is_some()
    }

    /// Generate a one-time token and open the browser auth URL.
    /// Returns the exchange_code to use for polling.
    pub async fn start_browser_auth(&self) -> Result<String, String> {
        let token_response = self
            .client
            .generate_token()
            .await
            .map_err(|e| format!("Failed to generate token: {}", e))?;

        let auth_url = format!(
            "{}?token={}&exchange={}",
            crate::api::client::CLI_BASE_URL,
            token_response.access_token,
            token_response.exchange_code
        );

        open::that(&auth_url).map_err(|e| format!("Failed to open browser: {}", e))?;

        Ok(token_response.exchange_code)
    }

    /// Poll the exchange endpoint until credentials arrive or a 5-minute timeout.
    /// Writes the final outcome into `callback_data`.
    pub async fn poll_for_credentials(
        &self,
        exchange_code: &str,
        callback_data: Arc<Mutex<CallbackData>>,
    ) -> Result<(), String> {
        let max_attempts = 150; // 5-minute timeout at 2-second intervals

        for _attempt in 0..max_attempts {
            tokio::time::sleep(Duration::from_secs(2)).await;

            match self.client.exchange_token(exchange_code).await {
                Ok(response) => {
                    if let (Some(cli_id), Some(api_key)) =
                        (response.cli_id.clone(), response.api_key.clone())
                    {
                        *callback_data.lock().unwrap() =
                            CallbackData::AuthReceived { cli_id, api_key };
                        return Ok(());
                    }

                    if let Some(status) = &response.status {
                        match status.as_str() {
                            "expired" => {
                                *callback_data.lock().unwrap() = CallbackData::Failed(
                                    "Authentication token expired".to_string(),
                                );
                                return Err("Authentication token expired".to_string());
                            }
                            "pending" => continue,
                            _ => {}
                        }
                    }

                    continue;
                }
                Err(_) => continue, // network error, retry
            }
        }

        *callback_data.lock().unwrap() =
            CallbackData::Failed("Authentication timeout".to_string());
        Err("Authentication timeout".to_string())
    }

    /// Persist the received credentials to the config file.
    pub async fn complete_auth_for_tui(
        &mut self,
        cli_id: &str,
        api_key: &str,
    ) -> Result<InitResult, String> {
        let config = Config {
            cli_id: cli_id.to_string(),
            api_key: Some(api_key.to_string()),
        };
        self.client.save_config(&config).map_err(|e| e.to_string())?;

        Ok(InitResult {
            cli_id: cli_id.to_string(),
            api_key: api_key.to_string(),
            message: format!("Initialized successfully! CLI ID: {}", cli_id),
        })
    }
}
