var API_URL = "https://ng_grok_url/api/webhook/sheets";


function handleEdit(e) {
  var range = e.range;
  var sheet = range.getSheet();


  // 1. FAIL FAST: Only run on "Sheet1" and ignore Header Row
  if (sheet.getName() !== "Sheet1" || range.getRow() === 1) return;


  // 2. BOUNDARY CHECK: Lock the table to the header width
  // This prevents the script from firing if you edit random cells in Column Z
  var lastCol = sheet.getLastColumn();
  if (range.getColumn() > lastCol) return;


  // 3. SETUP BATCHING VARIABLES
  var startRow = range.getRow();
  var numRows = range.getNumRows();
  var startCol = range.getColumn();
  var numCols = range.getNumColumns();
 
  // Get the headers to map Column Index -> Field Name (e.g., "Price")
  var headers = sheet.getRange(1, 1, 1, lastCol).getValues()[0];


  // Read the entire affected grid in ONE call (Super Fast)
  // We need the whole row data to check for UUIDs and reset values
  var fullRange = sheet.getRange(startRow, 1, numRows, lastCol);
  var values = fullRange.getValues(); // 2D Array of the edited rows


  var payload = [];
  var currentUser = Session.getActiveUser().getEmail() || "anonymous_sheet_user";
  var sheetUpdatesRequired = false; // Flag to write back to sheet only if needed


  // 4. ITERATE OVER AFFECTED ROWS
  for (var i = 0; i < numRows; i++) {
    var uuid = values[i][0]; // Column 1 is UUID (Index 0)
    var isNewRow = false;


    // --- LOGIC: HANDLE NEW ROWS / DRAG DOWN ---
    if (!uuid) {
      isNewRow = true;
      uuid = Utilities.getUuid(); // Mint new ID
      values[i][0] = uuid;       // Update our local data
      sheetUpdatesRequired = true;


      // As requested: Force Price/Quantity to 0 for new rows
      // (This prevents copying the price from the row above when dragging)
      var priceIndex = headers.indexOf("Price");
      var qtyIndex = headers.indexOf("Quantity");
     
      if (priceIndex > -1) values[i][priceIndex] = 0;
      if (qtyIndex > -1) values[i][qtyIndex] = 0;
    }


    // --- LOGIC: BUILD PAYLOAD ---
    // We iterate through all columns to capture the state
    for (var j = 0; j < headers.length; j++) {
      var fieldName = headers[j];
     
      // Skip irrelevant columns or empty headers
      if (!fieldName || fieldName === "Last Updated") continue;


      // Identify the value to send
      // If it's a new row, we use the values array (which has our resets)
      // If it's a standard edit, we also use the values array (which has the user's edit)
      var cellValue = values[i][j];


      // OPTIMIZATION: Only send fields that are actually being edited or are new
      // (Checking if the column falls within the user's edit range OR if it's a new row setup)
      var colIndex = j + 1;
      var isInsideEditRange = (colIndex >= startCol && colIndex < startCol + numCols);
     
      if (isNewRow || isInsideEditRange) {
        payload.push({
          "uuid": uuid.toString(),
          "field": fieldName,
          "value": cellValue,
          "user_email": currentUser
        });
      }
    }
  }


  // 5. WRITE BACK TO SHEET (If we generated UUIDs or reset Prices)
  if (sheetUpdatesRequired) {
    fullRange.setValues(values);
  }


  // 6. SEND BATCH TO BACKEND
  if (payload.length === 0) return;


  var options = {
    "method": "post",
    "contentType": "application/json",
    "payload": JSON.stringify(payload),
    "muteHttpExceptions": true
  };


  try {
    UrlFetchApp.fetch(API_URL, options);
  } catch (error) {
    Logger.log("Sync Failed: " + error.toString());
  }
}
