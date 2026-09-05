-- Deterministic probe for Mail 16 on macOS Sequoia 15.7.9.
-- Usage: osascript mail-category-probe.applescript view Transactions
--        osascript mail-category-probe.applescript inspect 15
--        osascript mail-category-probe.applescript membership 15
--        osascript mail-category-probe.applescript apply 15 Primary
-- apply changes the SENDER's category for existing and future messages.
-- All message operations require an already-read message to preserve read status.

on categoryNames()
 return {"Automatically", "Primary", "Transactions", "Updates", "Promotions"}
end categoryNames

on selectCategory(categoryName)
 if categoryName is not in {"All Mail", "Primary", "Transactions", "Updates", "Promotions"} then error "Unknown view"
 tell application "System Events" to tell process "Mail"
  set frontmost to true
  if (count of windows) is 0 then error "Mail window is unavailable. Open Inbox in the logged-in desktop and check Accessibility permission."
  -- Clicking the already-active category toggles to All Mail.
  set currentSummary to value of static text 2 of window 1
  if currentSummary does not start with (categoryName & " ·") then
   click (first button of splitter group 1 of splitter group 1 of window 1 whose description is categoryName)
  end if
 end tell
 delay 0.5
end selectCategory

on closeMenus()
 tell application "System Events" to tell process "Mail"
  key code 53
  key code 53
 end tell
end closeMenus

on openCategoryMenu(messageID)
 my selectCategory("All Mail")
 tell application "Mail"
  set targetMessage to first message of inbox whose id is messageID
  if read status of targetMessage is false then error "This operation requires an already-read inbox message to preserve unread status"
  set selected messages of message viewer 1 to {targetMessage}
 end tell
 delay 0.5
 tell application "System Events" to tell process "Mail"
  -- Essential: AppleScript selection alone leaves Categorize Sender disabled.
  set focused of table 1 of scroll area 1 of splitter group 1 of splitter group 1 of window 1 to true
  click menu bar item "Message" of menu bar 1
  set categoryItem to a reference to menu item "Categorize Sender" of menu "Message" of menu bar item "Message" of menu bar 1
  if enabled of categoryItem is false then error "Categorize Sender is disabled"
  click categoryItem
 end tell
 delay 0.2
end openCategoryMenu

on run argv
 if (count argv) < 2 then error "Usage: view CATEGORY | inspect MESSAGE_ID | membership MESSAGE_ID | apply MESSAGE_ID CATEGORY"
 set operation to item 1 of argv
 with timeout of 20 seconds
  if operation is "view" then
   my selectCategory(item 2 of argv)
   tell application "System Events" to tell process "Mail" to return value of static texts of window 1
  end if
  if operation is not in {"inspect", "membership", "apply"} then error "Unknown operation"
  set messageID to (item 2 of argv) as integer
  if operation is "membership" then
   tell application "Mail"
    set targetMessage to first message of inbox whose id is messageID
    if read status of targetMessage is false then error "This operation requires an already-read inbox message to preserve unread status"
   end tell
   set report to ""
   repeat with categoryName in {"Primary", "Transactions", "Updates", "Promotions"}
    my selectCategory(contents of categoryName)
    tell application "Mail" to set selected messages of message viewer 1 to {targetMessage}
    delay 0.3
    tell application "System Events" to tell process "Mail"
     -- AXSelectedRows avoids enumerating the entire mailbox's AXRows.
     set selectedCount to count of (value of attribute "AXSelectedRows" of table 1 of scroll area 1 of splitter group 1 of splitter group 1 of window 1)
    end tell
    set report to report & (contents of categoryName) & tab & (selectedCount > 0) & linefeed
   end repeat
   my selectCategory("Primary")
   tell application "Mail" to set selected messages of message viewer 1 to {}
   return report
  end if
  if operation is "apply" then
   if (count argv) is not 3 then error "apply needs MESSAGE_ID CATEGORY"
   set destination to item 3 of argv
   if destination is not in my categoryNames() then error "Unknown category"
  end if
  try
   my openCategoryMenu(messageID)
   if operation is "apply" then
    tell application "System Events" to tell process "Mail"
     click menu item destination of menu 1 of menu item "Categorize Sender" of menu "Message" of menu bar item "Message" of menu bar 1
    end tell
    -- Mail presents a separate AXDialog asynchronously, not a window sheet.
    -- Escape here would cancel the change before the verification pass.
    repeat 40 times
     tell application "System Events" to tell process "Mail"
      if subrole of window 1 is "AXDialog" then
       set dialogText to value of static texts of window 1
       if (count dialogText) < 1 then error "Unknown Mail confirmation dialog"
       set heading to item 1 of dialogText
       if heading does not start with "Recategorize All Messages from " or heading does not end with (" to " & destination) then error "Unexpected Mail recategorization confirmation"
       if not (exists button "Cancel" of window 1) or not (exists button "Continue" of window 1) then error "Unknown Mail confirmation buttons"
       click button "Continue" of window 1
       delay 1
       return "Sender recategorization confirmed; inspect again to verify."
      end if
     end tell
     delay 0.25
    end repeat
    return "No confirmation appeared; inspect again to verify the sender rule."
   end if
   set report to ""
   repeat with categoryName in my categoryNames()
    tell application "System Events" to tell process "Mail"
     set entry to a reference to menu item (contents of categoryName) of menu 1 of menu item "Categorize Sender" of menu "Message" of menu bar item "Message" of menu bar 1
     set mark to value of attribute "AXMenuItemMarkChar" of entry
     set isEnabled to enabled of entry
    end tell
    if mark is missing value then set mark to ""
    set report to report & (contents of categoryName) & tab & isEnabled & tab & mark & linefeed
   end repeat
   my closeMenus()
   return report
  on error errorText number errorNumber
   my closeMenus()
   error errorText number errorNumber
  end try
 end timeout
end run
