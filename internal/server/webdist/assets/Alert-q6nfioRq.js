import{U as D,dT as J,dU as h,cQ as u,W as I,aJ as l,ak as E,dV as K,V as Q,d as X,X as W,o as t,n as v,dW as Z,$ as q,aB as G,am as Y,z as H,s as ee,c as _,_ as g,aT as oe,ad as z,bA as re,aj as ne,ag as te,af as le,ae as se,ah as ae,a as ie,ar as ce,Z as A,c9 as de,a1 as i}from"./index-BMRouR67.js";function fe(e){const{lineHeight:s,borderRadius:b,fontWeightStrong:m,baseColor:r,dividerColor:S,actionColor:$,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:T,fontSize:k}=e;return{...J,fontSize:k,lineHeight:s,titleFontWeight:m,borderRadius:b,border:`1px solid ${S}`,color:$,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:b,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${h(r,u(o,{alpha:.25}))}`,colorInfo:h(r,u(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:C,closeIconColorInfo:p,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${h(r,u(y,{alpha:.25}))}`,colorSuccess:h(r,u(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:C,closeIconColorSuccess:p,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${h(r,u(P,{alpha:.33}))}`,colorWarning:h(r,u(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:C,closeIconColorWarning:p,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${h(r,u(T,{alpha:.25}))}`,colorError:h(r,u(T,{alpha:.08})),titleTextColorError:d,iconColorError:T,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:C,closeIconColorError:p,closeIconColorHoverError:x,closeIconColorPressedError:n}}const he={common:D,self:fe};var ue=I("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[l("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),E("closable",[I("alert-body",[l("title",`
 padding-right: 24px;
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),K({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
 position: absolute;
 left: 0;
 top: 0;
 align-items: center;
 justify-content: center;
 display: flex;
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 margin: var(--n-icon-margin);
 `),l("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),E("show-icon",[I("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),E("right-adjust",[I("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),I("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[l("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[Q("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const ge={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var ve=X({name:"Alert",inheritAttrs:!1,props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:b,inlineThemeDisabled:m,mergedRtlRef:r}=q(e),S=W("Alert","-alert",ue,he,e,s),$=G("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=S.value,{fontSize:y,borderRadius:P,titleFontWeight:T,lineHeight:k,iconSize:B,iconMargin:R,iconMarginRtl:w,closeIconSize:L,closeBorderRadius:V,closeSize:F,closeMargin:M,closeMarginRtl:j,padding:N}=o,{type:a}=e,{left:O,right:U}=de(R);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":V,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":k,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":T,"--n-icon-size":B,"--n-icon-margin":R,"--n-icon-margin-rtl":w,"--n-close-size":F,"--n-close-margin":M,"--n-close-margin-rtl":j,"--n-padding":N,"--n-icon-margin-left":O,"--n-icon-margin-right":U}}),c=m?Y("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),C=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:$,mergedClsPrefix:s,mergedBordered:b,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{C()},mergedTheme:S,cssVars:m?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),v(Z,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,b={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),_("div",A({key:1},A(this.$attrs,b)),[g(()=>this.closable&&(t(),v(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),g(()=>this.bordered&&(t(),_("div",{class:z(`${e}-alert__border`)},null,2))),g(()=>this.showIcon&&(t(),_("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[g(()=>re(s.icon,()=>[(t(),v(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),v(ae,{key:3});case"info":return t(),v(se,{key:4});case"warning":return t(),v(le,{key:5});case"error":return t(),v(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[g(()=>ce(s.header,m=>{const r=m||this.title;return r?(t(),_("div",{key:2,class:z(`${e}-alert-body__title`)},[g(()=>r)],2)):null})),g(()=>s.default&&(t(),_("div",{class:z(`${e}-alert-body__content`)},[g(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{ve as A};
