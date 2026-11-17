// Native integration test, compiled and run by the macOS menu CI job.
#import "../hints_darwin.m"
#include <assert.h>

@interface TestTarget : NSObject
@property(nonatomic) NSInteger calls;
- (void)invoke:(id)sender;
@end
@implementation TestTarget
- (void)invoke:(id)sender { self.calls++; }
@end

// Wails uses an NSMenu subclass. Preserve its identity, subclass behavior and
// original item instances while adding display-only equivalents.
@interface TestFrameworkMenu : NSMenu
@property(nonatomic) NSInteger lookups;
@end
@implementation TestFrameworkMenu
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    self.lookups++;
    return [super performKeyEquivalent:event];
}
@end

static NSEvent *keyEvent(NSString *key, NSEventModifierFlags flags) {
    return [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint
        modifierFlags:flags timestamp:0 windowNumber:0 context:nil characters:key
        charactersIgnoringModifiers:key isARepeat:NO keyCode:0];
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        TestTarget *target = [[[TestTarget alloc] init] autorelease];
        TestFrameworkMenu *root = [[[TestFrameworkMenu alloc] initWithTitle:@""] autorelease];
        root.autoenablesItems = NO;
        NSMenu *message = [[[NSMenu alloc] initWithTitle:@"Message"] autorelease];
        message.autoenablesItems = NO;
        NSMenuItem *top = [[[NSMenuItem alloc] initWithTitle:@"Message" action:nil keyEquivalent:@""] autorelease];
        top.submenu = message;
        [root addItem:top];
        NSMenuItem *reply = [message addItemWithTitle:@"Reply All" action:@selector(invoke:) keyEquivalent:@""];
        reply.target = target;
        NSMenuItem *toggle = [message addItemWithTitle:@"Toggle Read" action:@selector(invoke:) keyEquivalent:@""];
        toggle.target = target;
        NSMenuItem *copy = [message addItemWithTitle:@"Copy" action:@selector(invoke:) keyEquivalent:@"c"];
        copy.target = target; copy.keyEquivalentModifierMask = NSEventModifierFlagCommand;
        NSMenuItem *role = [message addItemWithTitle:@"Native Role" action:@selector(invoke:) keyEquivalent:@"q"];
        role.target = target; role.keyEquivalentModifierMask = NSEventModifierFlagCommand;
        NSArray *definitions = @[@{@"title":@"Message",@"children":@[
            @{@"title":@"Reply All",@"key":@"r",@"modifiers":@(NSEventModifierFlagCommand|NSEventModifierFlagShift)},
            @{@"title":@"Toggle Read",@"key":@"m",@"modifiers":@0}
        ]}];
        NSApp.mainMenu = root;
        decorateMenu(root,definitions);
        assert(NSApp.mainMenu == root && [root isKindOfClass:[TestFrameworkMenu class]]);
        assert([root itemAtIndex:0] == top && [message itemAtIndex:0] == reply);
        assert([reply.title isEqualToString:@"Reply All"] && [reply.keyEquivalent isEqualToString:@"r"]);
        assert(reply.keyEquivalentModifierMask == (NSEventModifierFlagCommand|NSEventModifierFlagShift));
        assert([toggle.keyEquivalent isEqualToString:@"m"] && toggle.keyEquivalentModifierMask == 0);
        assert(![root performKeyEquivalent:keyEvent(@"r",NSEventModifierFlagCommand|NSEventModifierFlagShift)]);
        assert(![root performKeyEquivalent:keyEvent(@"m",0)]);
        assert(target.calls == 0 && root.lookups == 2);
        assert([reply.keyEquivalent isEqualToString:@"r"] && [toggle.keyEquivalent isEqualToString:@"m"]);
        assert([root performKeyEquivalent:keyEvent(@"c",NSEventModifierFlagCommand)]);
        assert([root performKeyEquivalent:keyEvent(@"q",NSEventModifierFlagCommand)]);
        assert(target.calls == 2);
        // Menu clicks retain the exact original target/action.
        [message performActionForItemAtIndex:0];
        assert(target.calls == 3);
        // A queued Wails reinstall must not encounter an emptied original tree.
        Class decorated = object_getClass(root);
        NSApp.mainMenu = root;
        decorateMenu(root, definitions);
        assert(object_getClass(root) == decorated && root.numberOfItems == 1 && message.numberOfItems == 4);
        assert(![root performKeyEquivalent:keyEvent(@"r",NSEventModifierFlagCommand|NSEventModifierFlagShift)]);
        assert(target.calls == 3);
        // A later framework subclass must not cause recursive super dispatch.
        Class later = objc_allocateClassPair(decorated, "LaterFrameworkMenu", 0);
        objc_registerClassPair(later); object_setClass(root,later);
        assert(![root performKeyEquivalent:keyEvent(@"m",0)]);
        assert(target.calls == 3);
        // Direct submenu lookup must be as safe as application-root lookup.
        assert(![message performKeyEquivalent:keyEvent(@"m",0)]);
        assert(target.calls == 3);
        // Give every audited letter a real enabled menu equivalent before
        // exercising the window path; a no-binding typing test proves nothing.
        NSMutableArray *typingHints = [NSMutableArray array];
        for (NSString *letter in @[@"c",@"f",@"e",@"l",@"h",@"s",@"v",@"g",@"t"]) {
            NSString *title = [@"Typing " stringByAppendingString:letter];
            NSMenuItem *item = [message addItemWithTitle:title action:@selector(invoke:) keyEquivalent:@""];
            item.target = target;
            [typingHints addObject:@{@"title":title,@"key":letter,@"modifiers":@0}];
        }
        [typingHints addObject:@{@"title":@"Toggle Read",@"key":@"m",@"modifiers":@0}];
        decorateMenu(root,@[@{@"title":@"Message",@"children":typingHints}]);
        // Exercise the real NSWindow event path while typing into an editor.
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(100,100,500,200)
            styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO] autorelease];
        NSTextView *editor = [[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,500,200)] autorelease];
        window.contentView = editor;
        [window makeKeyAndOrderFront:nil];
        [window makeFirstResponder:editor];
        for (NSString *letter in @[@"c",@"f",@"e",@"l",@"h",@"m",@"s",@"v",@"g",@"t"]) {
            NSEvent *event = [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint
                modifierFlags:0 timestamp:0 windowNumber:window.windowNumber context:nil
                characters:letter charactersIgnoringModifiers:letter isARepeat:NO keyCode:0];
            [window sendEvent:event];
        }
        assert([editor.string isEqualToString:@"cfelhmsvgt"]);
        assert(target.calls == 3);
        [window orderOut:nil];
        while (message.numberOfItems > 4) [message removeItemAtIndex:message.numberOfItems-1];
        decorateMenu(root,definitions);
        // Disabled native actions remain disabled.
        copy.enabled = NO;
        // AppKit can return YES for a disabled match (consumed, not invoked).
        // The invariant is that its target must not run, not event propagation.
        [root performKeyEquivalent:keyEvent(@"c",NSEventModifierFlagCommand)];
        assert(target.calls == 3);
        if (getenv("POSTI_MENU_SCREENSHOT")) {
            // Capture an actual AppKit popup when the runner has a WindowServer.
            // Capture failure is nonfatal; assertions above remain mandatory.
            [NSApp activateIgnoringOtherApps:YES];
            NSTimer *timer = [NSTimer timerWithTimeInterval:1 repeats:NO block:^(NSTimer *timer) {
                NSTask *capture = [[[NSTask alloc] init] autorelease];
                capture.launchPath = @"/usr/sbin/screencapture";
                capture.arguments = @[@"-x", [NSString stringWithUTF8String:getenv("POSTI_MENU_SCREENSHOT")]];
                [capture launch]; [capture waitUntilExit];
                [message cancelTracking];
            }];
            [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSRunLoopCommonModes];
            [message popUpMenuPositioningItem:nil atLocation:NSMakePoint(150,500) inView:nil];
        }
        puts("AppKit shortcut column and keyboard ownership tests passed");
    }
    return 0;
}
