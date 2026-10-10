import{a1 as q,fd as K,fe as u,dT as g,a3 as I,aV as l,av as E,ff as G,a2 as J,d as Q,a4 as W,o as t,x as v,fg as U,a7 as X,aN as Y,ax as Z,g as H,p as ee,c as S,a6 as h,b4 as oe,ao as z,cA as re,au as ne,ar as te,aq as le,ap as se,as as ae,a as ie,aD as ce,a5 as A,dc as de,a9 as i}from"./index-BdMfu373.js";function fe(e){const{lineHeight:s,borderRadius:b,fontWeightStrong:p,baseColor:r,dividerColor:_,actionColor:$,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:C,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:T,fontSize:k}=e;return{...K,fontSize:k,lineHeight:s,titleFontWeight:p,borderRadius:b,border:`1px solid ${_}`,color:$,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:b,closeColorHover:f,closeColorPressed:C,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${u(r,g(o,{alpha:.25}))}`,colorInfo:u(r,g(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:C,closeIconColorInfo:m,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${u(r,g(y,{alpha:.25}))}`,colorSuccess:u(r,g(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:C,closeIconColorSuccess:m,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${u(r,g(P,{alpha:.33}))}`,colorWarning:u(r,g(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:C,closeIconColorWarning:m,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${u(r,g(T,{alpha:.25}))}`,colorError:u(r,g(T,{alpha:.08})),titleTextColorError:d,iconColorError:T,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:C,closeIconColorError:m,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ue={common:q,self:fe};var ge=I("alert",`
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
 `,[J("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const he={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var ve=Q({name:"Alert",inheritAttrs:!1,props:he,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:b,inlineThemeDisabled:p,mergedRtlRef:r}=X(e),_=W("Alert","-alert",ge,ue,e,s),$=Y("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:T,lineHeight:k,iconSize:w,iconMargin:R,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:V,closeMarginRtl:j,padding:N}=o,{type:a}=e,{left:D,right:O}=de(R);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":k,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":T,"--n-icon-size":w,"--n-icon-margin":R,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":V,"--n-close-margin-rtl":j,"--n-padding":N,"--n-icon-margin-left":D,"--n-icon-margin-right":O}}),c=p?Z("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),C=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:$,mergedClsPrefix:s,mergedBordered:b,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{C()},mergedTheme:_,cssVars:p?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),v(U,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,b={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),S("div",A({key:1},A(this.$attrs,b)),[h(()=>this.closable&&(t(),v(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),h(()=>this.bordered&&(t(),S("div",{class:z(`${e}-alert__border`)},null,2))),h(()=>this.showIcon&&(t(),S("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[h(()=>re(s.icon,()=>[(t(),v(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),v(ae,{key:3});case"info":return t(),v(se,{key:4});case"warning":return t(),v(le,{key:5});case"error":return t(),v(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[h(()=>ce(s.header,p=>{const r=p||this.title;return r?(t(),S("div",{key:2,class:z(`${e}-alert-body__title`)},[h(()=>r)],2)):null})),h(()=>s.default&&(t(),S("div",{class:z(`${e}-alert-body__content`)},[h(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{ve as A};
