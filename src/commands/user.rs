use anyhow::Result;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use crate::api::{CloudCentClient, Config};

/// Callback data for new auth flow
#[derive(Debug, Clone)]
pub enum CallbackData {
    Pending,
    Received { cli_id: String, api_key: String },
    Failed(String),
}

/// User initialization result
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

    pub fn client(&self) -> &CloudCentClient {
        &self.client
    }

    pub fn client_mut(&mut self) -> &mut CloudCentClient {
        &mut self.client
    }

    pub fn is_initialized(&self) -> bool {
        self.client.get_config().is_some()
    }

    /// Start browser auth flow for TUI - returns exchange_code and callback data
    pub async fn start_browser_auth_for_tui(&self) -> Result<(String, Arc<Mutex<CallbackData>>), String> {
        // 1. 调用 /api/auth/generate-token 获取 access_token 和 exchange_code
        let token_response = self.client
            .generate_token()
            .await
            .map_err(|e| format!("Failed to generate token: {}", e))?;
        
        let callback_data = Arc::new(Mutex::new(CallbackData::Pending));
        
        // 2. 打开浏览器，传递 token 和 exchange
        let auth_url = format!(
            "{}?token={}&exchange={}",
            crate::api::client::CLI_BASE_URL,
            token_response.access_token,
            token_response.exchange_code
        );
        
        open::that(&auth_url)
            .map_err(|e| format!("Failed to open browser: {}", e))?;
        
        Ok((token_response.exchange_code, callback_data))
    }
    
    /// Poll for credentials using exchange_code
    pub async fn poll_for_credentials(&self, exchange_code: &str, callback_data: Arc<Mutex<CallbackData>>) -> Result<(), String> {
        // 轮询 /api/auth/exchange 获取凭证（每2秒一次）
        let max_attempts = 150; // 5 分钟超时
        
        for _attempt in 0..max_attempts {
            tokio::time::sleep(Duration::from_secs(2)).await;
            
            match self.client.exchange_token(exchange_code).await {
                Ok(response) => {
                    // 如果有 cli_id 和 api_key，说明认证完成
                    if let (Some(cli_id), Some(api_key)) = (response.cli_id.clone(), response.api_key.clone()) {
                        *callback_data.lock().unwrap() = CallbackData::Received {
                            cli_id,
                            api_key,
                        };
                        return Ok(());
                    }
                    
                    // 检查 status 字段
                    if let Some(status) = &response.status {
                        match status.as_str() {
                            "expired" => {
                                *callback_data.lock().unwrap() = CallbackData::Failed(
                                    "Authentication token expired".to_string()
                                );
                                return Err("Authentication token expired".to_string());
                            }
                            "pending" => {
                                continue;
                            }
                            _ => {}
                        }
                    }
                    
                    // 如果没有凭证也没有明确的状态，继续轮询
                    continue;
                }
                Err(_e) => {
                    // 网络错误，继续重试
                    continue;
                }
            }
        }
        
        *callback_data.lock().unwrap() = CallbackData::Failed(
            "Authentication timeout".to_string()
        );
        Err("Authentication timeout".to_string())
    }
    
    /// Complete auth with token for TUI
    pub async fn complete_auth_for_tui(&mut self, cli_id: &str, api_key: &str) -> Result<InitResult, String> {
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
