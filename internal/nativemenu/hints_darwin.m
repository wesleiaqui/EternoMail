//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <objc/message.h>
#import <objc/runtime.h>

static char shortcutHintsKey;
static char shortcutBaseClassKey;
static char shortcutLookupKey;

// Native equivalents supply AppKit's shortcut column, but the webview still
// owns these bindings. Hide only hints during native keyboard lookup, restoring
// them synchronously before drawing resumes. Clipboard and role shortcuts stay.
static BOOL performHintKeyEquivalent(NSMenu *menu, SEL selector, NSEvent *event) {
    struct objc_super parent = {menu, (Class)objc_getAssociatedObject(menu, &shortcutBaseClassKey)};
    if ([objc_getAssociatedObject(menu, &shortcutLookupKey) boolValue]) {
        return ((BOOL (*)(struct objc_super *, SEL, NSEvent *))objc_msgSendSuper)(&parent, selector, event);
    }
    objc_setAssociatedObject(menu, &shortcutLookupKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    NSArray *hints = [objc_getAssociatedObject(menu, &shortcutHintsKey) retain];
    NSMutableArray *previous = [NSMutableArray array];
    for (NSDictionary *hint in hints) {
        NSMenuItem *item = hint[@"item"];
        [previous addObject:@{@"item":item, @"key":item.keyEquivalent, @"modifiers":@(item.keyEquivalentModifierMask)}];
        item.keyEquivalent = @"";
    }
    @try {
        return ((BOOL (*)(struct objc_super *, SEL, NSEvent *))objc_msgSendSuper)(&parent, selector, event);
    } @finally {
        for (NSDictionary *hint in previous) {
            NSMenuItem *item = hint[@"item"];
            item.keyEquivalent = hint[@"key"];
            item.keyEquivalentModifierMask = [hint[@"modifiers"] unsignedIntegerValue];
        }
        [hints release];
        objc_setAssociatedObject(menu, &shortcutLookupKey, nil, OBJC_ASSOCIATION_ASSIGN);
    }
}

static void decorateMenu(NSMenu *menu, NSArray *definitions);

static void applyHints(NSMenu *menu, NSArray *definitions, NSMutableArray *hints) {
    for (NSDictionary *definition in definitions) {
        NSMenuItem *item = nil;
        for (NSMenuItem *candidate in menu.itemArray) {
            if (![candidate.title isEqualToString:definition[@"title"]]) continue;
            if (item) { item = nil; break; } // Ambiguous labels fail closed.
            item = candidate;
        }
        if (!item) continue;
        if (item.submenu) {
            decorateMenu(item.submenu, definition[@"children"]);
            [hints addObjectsFromArray:objc_getAssociatedObject(item.submenu, &shortcutHintsKey) ?: @[]];
        }
        NSString *key = definition[@"key"];
        if (!key.length) continue;
        item.keyEquivalent = key;
        item.keyEquivalentModifierMask = [definition[@"modifiers"] unsignedIntegerValue];
        [hints addObject:@{@"item":item, @"key":key, @"modifiers":@(item.keyEquivalentModifierMask)}];
    }
}

// Keep Wails' original root and complete item tree in place. Its context keeps
// the root pointer and can reinstall it, so moving items to a replacement menu
// would empty the application menu on queued updates. An ivar-free, per-class
// subclass guards each menu's lookup, including direct submenu calls. It does not
// change WailsMenu globally.
static void decorateMenu(NSMenu *menu, NSArray *definitions) {
    if (!menu) return;
    if (!objc_getAssociatedObject(menu, &shortcutHintsKey)) {
        Class original = object_getClass(menu);
        NSString *name = [@"PostilaatikkoHints_" stringByAppendingString:NSStringFromClass(original)];
        Class decorated = NSClassFromString(name);
        if (!decorated) {
            decorated = objc_allocateClassPair(original, name.UTF8String, 0);
            Method method = class_getInstanceMethod(original, @selector(performKeyEquivalent:));
            class_addMethod(decorated, @selector(performKeyEquivalent:), (IMP)performHintKeyEquivalent, method_getTypeEncoding(method));
            objc_registerClassPair(decorated);
        }
        objc_setAssociatedObject(menu, &shortcutBaseClassKey, original, OBJC_ASSOCIATION_ASSIGN);
        object_setClass(menu, decorated);
    }
    NSMutableArray *hints = [NSMutableArray array];
    applyHints(menu, definitions, hints);
    objc_setAssociatedObject(menu, &shortcutHintsKey, hints, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
}

// Wails queues whole-menu replacement on the main queue. Follow it on that same
// queue and capture the current tree, including contextual Event/Contact menus.
void postilaatikkoDecorateMenu(const char *json) {
    @autoreleasepool {
    NSData *data = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    NSArray *definitions = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (![definitions isKindOfClass:[NSArray class]]) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool { decorateMenu(NSApp.mainMenu, definitions); }
    });
    }
}
