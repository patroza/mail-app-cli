#import <Foundation/Foundation.h>
#import <objc/message.h>
#import <dlfcn.h>
@interface Probe:NSObject
@property(copy) NSString *label;
@end
@implementation Probe
- (NSMethodSignature *)methodSignatureForSelector:(SEL)s {
 NSString *n=NSStringFromSelector(s); NSUInteger count=[[n componentsSeparatedByString:@":"] count]-1;
 return [NSMethodSignature signatureWithObjCTypes:[[@"@@:" stringByAppendingString:[@"" stringByPaddingToLength:count withString:@"@" startingAtIndex:0]] UTF8String]];
}
- (void)forwardInvocation:(NSInvocation *)inv {
 NSMutableArray *args=[NSMutableArray array];
 for(NSUInteger i=2;i<inv.methodSignature.numberOfArguments;i++){__unsafe_unretained id v=nil;[inv getArgument:&v atIndex:i];[args addObject:v?:[NSNull null]];}
 printf("%s %s %s\n",self.label.UTF8String,NSStringFromSelector(inv.selector).UTF8String,args.description.UTF8String);
 id result=nil;[inv setReturnValue:&result];
}
@end
int main(){@autoreleasepool{
 dlopen("/System/Library/PrivateFrameworks/EmailDaemon.framework/EmailDaemon",RTLD_NOW);
 Probe *a=[Probe new],*b=[Probe new],*c=[Probe new];a.label=@"messages";b.label=@"global";c.label=@"businessAddresses";
 ((void(*)(id,SEL,id,id,id))objc_msgSend)(NSClassFromString(@"EDCategoryPersistence"),NSSelectorFromString(@"addCategoryColumnsToMessagesSelectComponent:globalMessagesSelectComponent:businessAddressesSelectComponent:"),a,b,c);
}}
