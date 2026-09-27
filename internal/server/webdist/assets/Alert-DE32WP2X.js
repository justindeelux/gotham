import{M as Q,bP as q,bQ as u,bR as h,O as I,am as l,a7 as $,bS as D,N as G,d as U,P as W,o as t,l as b,bT as Z,Q as J,U as X,aa as Y,y as H,q as ee,c as T,G as g,aE as oe,E as z,Z as re,_ as ne,a4 as te,a3 as le,a2 as se,a5 as ae,a as ie,H as ce,ap as A,aK as de,ab as i}from"./index-CeE65dYe.js";function fe(e){const{lineHeight:s,borderRadius:v,fontWeightStrong:m,baseColor:r,dividerColor:_,actionColor:E,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:S,fontSize:R}=e;return{...q,fontSize:R,lineHeight:s,titleFontWeight:m,borderRadius:v,border:`1px solid ${_}`,color:E,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:v,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${u(r,h(o,{alpha:.25}))}`,colorInfo:u(r,h(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:C,closeIconColorInfo:p,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${u(r,h(y,{alpha:.25}))}`,colorSuccess:u(r,h(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:C,closeIconColorSuccess:p,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${u(r,h(P,{alpha:.33}))}`,colorWarning:u(r,h(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:C,closeIconColorWarning:p,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${u(r,h(S,{alpha:.25}))}`,colorError:u(r,h(S,{alpha:.08})),titleTextColorError:d,iconColorError:S,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:C,closeIconColorError:p,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ue={common:Q,self:fe};var he=I("alert",`
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
 `),$("closable",[I("alert-body",[l("title",`
 padding-right: 24px;
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),D({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
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
 `),$("show-icon",[I("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),$("right-adjust",[I("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),I("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[l("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[G("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const ge={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var be=U({name:"Alert",inheritAttrs:!1,props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:v,inlineThemeDisabled:m,mergedRtlRef:r}=J(e),_=W("Alert","-alert",he,ue,e,s),E=X("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:S,lineHeight:R,iconSize:w,iconMargin:k,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:M,closeSize:F,closeMargin:V,closeMarginRtl:j,padding:N}=o,{type:a}=e,{left:O,right:K}=de(k);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":M,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":R,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":S,"--n-icon-size":w,"--n-icon-margin":k,"--n-icon-margin-rtl":B,"--n-close-size":F,"--n-close-margin":V,"--n-close-margin-rtl":j,"--n-padding":N,"--n-icon-margin-left":O,"--n-icon-margin-right":K}}),c=m?Y("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),C=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:E,mergedClsPrefix:s,mergedBordered:v,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{C()},mergedTheme:_,cssVars:m?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),b(Z,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,v={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),T("div",A({key:1},A(this.$attrs,v)),[g(()=>this.closable&&(t(),b(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),g(()=>this.bordered&&(t(),T("div",{class:z(`${e}-alert__border`)},null,2))),g(()=>this.showIcon&&(t(),T("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[g(()=>re(s.icon,()=>[(t(),b(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),b(ae,{key:3});case"info":return t(),b(se,{key:4});case"warning":return t(),b(le,{key:5});case"error":return t(),b(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[g(()=>ce(s.header,m=>{const r=m||this.title;return r?(t(),T("div",{key:2,class:z(`${e}-alert-body__title`)},[g(()=>r)],2)):null})),g(()=>s.default&&(t(),T("div",{class:z(`${e}-alert-body__content`)},[g(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{be as A};
