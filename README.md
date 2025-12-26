# .env example:

### GOOGLE_CLIENT_ID=
### GOOGLE_CLIENT_SECRET=
### SPREADSHEET_ID=
### DB_HOST=127.0.0.1

# Sheets setup
1. Copy code.gs from browser-script into extensions->AppScript>code.gs (Ensure your spreadsheet is named Sheet1)
2. Ensure your sheet is empty too
3. Setup Ngrok event listener for this to work please too
 
# Backend Setup
1. In /backend dir, run docker-compose-up --build

# Usage
1. In frontend navigate to '/' and signIn

## I tried hosting it but no free tier was available and much time isn't left to go on AWS EC2, sorry for this.