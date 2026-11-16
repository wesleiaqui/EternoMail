//go:build darwin && cgo

#import <Cocoa/Cocoa.h>

void postilaatikkoClipboard(int command) {
  dispatch_async(dispatch_get_main_queue(), ^{
    SEL action = NULL;
    switch (command) {
      case 1: action = @selector(cut:); break;
      case 2: action = @selector(copy:); break;
      case 3: action = @selector(paste:); break;
      case 4: action = @selector(selectAll:); break;
    }
    if (action != NULL) [NSApp sendAction:action to:nil from:nil];
  });
}
