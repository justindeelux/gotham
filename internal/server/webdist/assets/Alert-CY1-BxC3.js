import{H as q,cx as D,cy as u,bG as h,J as I,am as l,a0 as E,cz as G,I as J,d as Q,K as W,o as t,m as v,cA as X,O as Y,ad as Z,a2 as U,y as R,q as ee,c as T,N as g,aw as oe,V as z,aF as re,$ as ne,Y as te,X as le,W as se,Z as ae,a as ie,a5 as ce,M as A,b6 as de,Q as i}from"./index-pcbtZsmD.js";function fe(e){const{lineHeight:s,borderRadius:b,fontWeightStrong:m,baseColor:r,dividerColor:_,actionColor:$,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:S,fontSize:k}=e;return{...D,fontSize:k,lineHeight:s,titleFontWeight:m,borderRadius:b,border:`1px solid ${_}`,color:$,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:b,closeColorHover:f,closeColorPressed:C,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${u(r,h(o,{alpha:.25}))}`,colorInfo:u(r,h(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:C,closeIconColorInfo:p,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${u(r,h(y,{alpha:.25}))}`,colorSuccess:u(r,h(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:C,closeIconColorSuccess:p,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${u(r,h(P,{alpha:.33}))}`,colorWarning:u(r,h(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:C,closeIconColorWarning:p,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${u(r,h(S,{alpha:.25}))}`,colorError:u(r,h(S,{alpha:.08})),titleTextColorError:d,iconColorError:S,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:C,closeIconColorError:p,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ue={common:q,self:fe};var he=I("alert",`
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
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),G({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
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
 `,[J("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const ge={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var ve=Q({name:"Alert",inheritAttrs:!1,props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:b,inlineThemeDisabled:m,mergedRtlRef:r}=Y(e),_=W("Alert","-alert",he,ue,e,s),$=Z("Alert",r,s),d=R(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:S,lineHeight:k,iconSize:w,iconMargin:H,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:V,closeMarginRtl:j,padding:N}=o,{type:a}=e,{left:O,right:K}=de(H);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":k,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":S,"--n-icon-size":w,"--n-icon-margin":H,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":V,"--n-close-margin-rtl":j,"--n-padding":N,"--n-icon-margin-left":O,"--n-icon-margin-right":K}}),c=m?U("alert",R(()=>e.type[0]),d,e):void 0,f=ee(!0),C=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:$,mergedClsPrefix:s,mergedBordered:b,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{C()},mergedTheme:_,cssVars:m?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),v(X,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,b={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),T("div",A({key:1},A(this.$attrs,b)),[g(()=>this.closable&&(t(),v(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),g(()=>this.bordered&&(t(),T("div",{class:z(`${e}-alert__border`)},null,2))),g(()=>this.showIcon&&(t(),T("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[g(()=>re(s.icon,()=>[(t(),v(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),v(ae,{key:3});case"info":return t(),v(se,{key:4});case"warning":return t(),v(le,{key:5});case"error":return t(),v(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[g(()=>ce(s.header,m=>{const r=m||this.title;return r?(t(),T("div",{key:2,class:z(`${e}-alert-body__title`)},[g(()=>r)],2)):null})),g(()=>s.default&&(t(),T("div",{class:z(`${e}-alert-body__content`)},[g(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{ve as A};
